package checks

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

var (
	goEnvReadPattern     = regexp.MustCompile(`\bos\.(?:Getenv|LookupEnv)\s*\(\s*(?:"([A-Za-z_][A-Za-z0-9_]*)"|` + "`" + `([A-Za-z_][A-Za-z0-9_]*)` + "`" + `)\s*\)`)
	nodeEnvReadPattern   = regexp.MustCompile(`\bprocess\.env\.([A-Za-z_][A-Za-z0-9_]*)|\bprocess\.env\[\s*["']([A-Za-z_][A-Za-z0-9_]*)["']\s*\]`)
	pythonEnvReadPattern = regexp.MustCompile(`\bos\.getenv\s*\(\s*["']([A-Za-z_][A-Za-z0-9_]*)["']|\bos\.environ(?:\.get\s*\(|\s*\[\s*)["']([A-Za-z_][A-Za-z0-9_]*)["']`)
	envKeyPattern        = regexp.MustCompile(`(?m)^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	awsSecretPattern     = regexp.MustCompile(`(?:AKIA|ASIA)[A-Z0-9]{16}`)
	openAISecretPattern  = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)
	githubSecretPattern  = regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`)
	privateKeyPattern    = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
)

type environmentCheck struct{}

type envReference struct {
	Name string
	File string
	Line int
}

func (environmentCheck) ID() string   { return "7" }
func (environmentCheck) Name() string { return ".env.example / secrets" }

func (environmentCheck) Run(_ context.Context, r *repo.Repo) Result {
	result := Result{CheckID: "7"}
	references := collectEnvironmentReferences(r)
	exampleKeys := collectExampleKeys(r)
	missing := make(map[string]envReference)
	for _, reference := range references {
		if _, documented := exampleKeys[reference.Name]; !documented {
			missing[reference.Name] = reference
		}
	}

	fixable := false
	if r.GitAvailable {
		for _, file := range r.TrackedFiles {
			if !isSensitiveEnvFile(file) {
				continue
			}
			result.Status = StatusFail
			result.Findings = append(result.Findings, Finding{
				File:    file,
				Message: "a .env file is tracked by Git",
				Hint:    "remove this file from Git and add it to .gitignore",
			})
		}
	}

	for _, file := range r.Files {
		if isDependencyFile(file) {
			continue
		}
		contents, err := r.Read(file)
		if err != nil {
			continue
		}
		for lineNumber, line := range strings.Split(string(contents), "\n") {
			if hasSecret(line) {
				result.Status = StatusFail
				result.Findings = append(result.Findings, Finding{
					File:    file,
					Line:    lineNumber + 1,
					Message: "hard-coded secret pattern detected",
					Hint:    "remove the secret, rotate it if real, and use an environment variable",
				})
			}
		}
	}

	missingNames := make([]string, 0, len(missing))
	for name := range missing {
		missingNames = append(missingNames, name)
	}
	sort.Strings(missingNames)
	for _, name := range missingNames {
		reference := missing[name]
		result.Status = StatusFail
		result.Findings = append(result.Findings, Finding{
			File:    reference.File,
			Line:    reference.Line,
			Message: fmt.Sprintf("%s is read from code but missing from .env.example", name),
			Hint:    "add the variable name with an empty value to .env.example",
		})
	}
	if len(missingNames) > 0 {
		fixable = true
	}
	result.Fixable = fixable

	if result.Status == StatusFail {
		return result
	}
	result.Status = StatusPass
	result.Findings = []Finding{{Message: "no tracked .env, hard-coded secret, or undocumented environment variable found"}}
	return result
}

// Match environment files at any depth, without flagging distributable templates.
func isSensitiveEnvFile(file string) bool {
	name := path.Base(file) // repository paths use forward slashes on every OS
	if name == ".env" {
		return true
	}
	if !strings.HasPrefix(name, ".env.") {
		return false
	}
	switch name {
	case ".env.example", ".env.sample", ".env.template", ".env.dist":
		return false
	default:
		return true
	}
}

func collectEnvironmentReferences(r *repo.Repo) []envReference {
	seen := make(map[string]envReference)
	for _, file := range r.Files {
		if !isSourceFile(file) {
			continue
		}
		contents, err := r.Read(file)
		if err != nil {
			continue
		}
		text := stripFullLineComments(string(contents))
		patterns := []*regexp.Regexp{}
		switch sourceExtension(file) {
		case ".go":
			patterns = []*regexp.Regexp{goEnvReadPattern}
		case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
			patterns = []*regexp.Regexp{nodeEnvReadPattern}
		case ".py":
			patterns = []*regexp.Regexp{pythonEnvReadPattern}
		}
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatchIndex(text, -1) {
				name := firstCapture(text, match)
				if name == "" {
					continue
				}
				line := strings.Count(text[:match[0]], "\n") + 1
				if _, exists := seen[name]; !exists {
					seen[name] = envReference{Name: name, File: file, Line: line}
				}
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for name := range seen {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	references := make([]envReference, 0, len(keys))
	for _, name := range keys {
		references = append(references, seen[name])
	}
	return references
}

// DetectedEnvironmentVariables returns deterministic variable names found in
// supported source files. It is shared by fix so .env.example generation uses
// the same detection rules as the audit.
func DetectedEnvironmentVariables(r *repo.Repo) []string {
	references := collectEnvironmentReferences(r)
	names := make([]string, 0, len(references))
	for _, reference := range references {
		names = append(names, reference.Name)
	}
	return names
}

func collectExampleKeys(r *repo.Repo) map[string]bool {
	keys := make(map[string]bool)
	if !repo.HasFile(r.Files, ".env.example") {
		return keys
	}
	contents, err := r.Read(".env.example")
	if err != nil {
		return keys
	}
	for _, match := range envKeyPattern.FindAllStringSubmatch(string(contents), -1) {
		if len(match) > 1 {
			keys[match[1]] = true
		}
	}
	return keys
}

func hasSecret(line string) bool {
	return awsSecretPattern.MatchString(line) || openAISecretPattern.MatchString(line) || githubSecretPattern.MatchString(line) || privateKeyPattern.MatchString(line)
}

func isSourceFile(file string) bool {
	if isDependencyFile(file) {
		return false
	}
	switch sourceExtension(file) {
	case ".go", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".py":
		return true
	default:
		return false
	}
}

func isDependencyFile(file string) bool {
	return strings.HasPrefix(file, "vendor/") || strings.HasPrefix(file, "third_party/")
}

func sourceExtension(file string) string {
	index := strings.LastIndex(file, ".")
	if index < 0 {
		return ""
	}
	return strings.ToLower(file[index:])
}

func stripFullLineComments(contents string) string {
	lines := strings.Split(contents, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			lines[index] = ""
		}
	}
	return strings.Join(lines, "\n")
}

func firstCapture(contents string, indexes []int) string {
	for index := 2; index+1 < len(indexes); index += 2 {
		start, end := indexes[index], indexes[index+1]
		if start >= 0 && end >= 0 {
			return contents[start:end]
		}
	}
	return ""
}
