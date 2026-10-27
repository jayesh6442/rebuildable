package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

type testCommandCheck struct{}

type TestCommand struct {
	Args        []string
	Description string
}

func (testCommandCheck) ID() string   { return "5" }
func (testCommandCheck) Name() string { return "One test command" }

func (testCommandCheck) Run(_ context.Context, r *repo.Repo) Result {
	result := Result{CheckID: "5"}
	if repo.HasFile(r.Files, "Makefile") {
		if line, command := makeTestTarget(r); command != "" {
			result.Status = StatusPass
			result.Findings = []Finding{{File: "Makefile", Line: line, Message: fmt.Sprintf("test target runs %s", command)}}
			return result
		}
	}

	if repo.HasFile(r.Files, "package.json") {
		if line, script, status := nodeTestScript(r); script != "" {
			result.Status = status
			result.Findings = []Finding{{File: "package.json", Line: line, Message: fmt.Sprintf("scripts.test runs %s", script)}}
			if status == StatusFail {
				result.Findings[0].Hint = "replace npm's placeholder with a real test command"
			}
			return result
		}
	}

	for _, file := range r.Files {
		if strings.HasSuffix(file, "_test.go") {
			result.Status = StatusPass
			result.Findings = []Finding{{File: file, Message: "Go test file found; run go test ./..."}}
			return result
		}
	}

	if hasPytestConfig(r) && hasPythonTestFile(r) {
		result.Status = StatusPass
		result.Findings = []Finding{{File: "pytest configuration", Message: "pytest configuration and test files found; run pytest"}}
		return result
	}

	result.Status = StatusFail
	result.Fixable = canGenerateTestCommand(r)
	result.Findings = []Finding{{
		File:    "Makefile",
		Message: "no recognized test command found",
		Hint:    "add a Makefile test target or wire the detected ecosystem's test runner",
	}}
	return result
}

// DetectTestCommand is the shared command contract used by audit, fix, and
// verify. It returns an argument vector rather than a shell string so callers
// never need to execute untrusted shell text on the host.
func DetectTestCommand(r *repo.Repo) (TestCommand, bool) {
	if repo.HasFile(r.Files, "package.json") {
		if _, script, status := nodeTestScript(r); status == StatusPass && script != "" {
			return TestCommand{Args: []string{nodePackageManager(r), "test"}, Description: script}, true
		}
	}
	for _, file := range r.Files {
		if strings.HasSuffix(file, "_test.go") {
			return TestCommand{Args: []string{"go", "test", "./..."}, Description: "go test ./..."}, true
		}
	}
	if hasPytestConfig(r) && hasPythonTestFile(r) {
		return TestCommand{Args: []string{"python", "-m", "pytest"}, Description: "python -m pytest"}, true
	}
	if repo.HasFile(r.Files, "Makefile") {
		if _, command := makeTestTarget(r); command != "" {
			return TestCommand{Args: []string{"make", "test"}, Description: "make test"}, true
		}
	}
	return TestCommand{}, false
}

func canGenerateTestCommand(r *repo.Repo) bool {
	if _, ok := DetectTestCommand(r); ok {
		return false
	}
	return repo.HasFile(r.Files, "go.mod")
}

func nodePackageManager(r *repo.Repo) string {
	if contents, err := r.Read("package.json"); err == nil {
		var packageJSON struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(contents, &packageJSON) == nil && strings.Contains(packageJSON.PackageManager, "@") {
			manager := strings.SplitN(packageJSON.PackageManager, "@", 2)[0]
			if manager == "npm" || manager == "pnpm" || manager == "yarn" || manager == "bun" {
				return manager
			}
		}
	}
	for _, candidate := range []struct {
		file    string
		manager string
	}{
		{file: "pnpm-lock.yaml", manager: "pnpm"},
		{file: "yarn.lock", manager: "yarn"},
		{file: "bun.lock", manager: "bun"},
	} {
		if repo.HasFile(r.Files, candidate.file) {
			return candidate.manager
		}
	}
	return "npm"
}

func makeTestTarget(r *repo.Repo) (int, string) {
	contents, err := r.Read("Makefile")
	if err != nil {
		return 0, ""
	}
	lines := strings.Split(string(contents), "\n")
	for index, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if !strings.HasPrefix(line, "test:") && !strings.HasPrefix(line, "test ") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		// Make only treats text after ";" on the rule line as a recipe; anything
		// before it is a prerequisite list, not a command.
		prerequisites, inline, _ := strings.Cut(strings.SplitN(parts[1], "#", 2)[0], ";")
		if inline = strings.TrimSpace(inline); inline != "" {
			return index + 1, inline
		}
		for _, recipe := range lines[index+1:] {
			if strings.TrimSpace(recipe) == "" {
				continue
			}
			if len(recipe) == 0 || (recipe[0] != '\t' && recipe[0] != ' ') {
				break
			}
			command := strings.TrimSpace(strings.SplitN(recipe, "#", 2)[0])
			if command != "" {
				return index + 1, command
			}
		}
		if prerequisites = strings.TrimSpace(prerequisites); prerequisites != "" {
			return index + 1, prerequisites
		}
	}
	return 0, ""
}

func nodeTestScript(r *repo.Repo) (int, string, Status) {
	contents, err := r.Read("package.json")
	if err != nil {
		return 0, "", StatusFail
	}
	var packageJSON struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(contents, &packageJSON); err != nil {
		return 1, "package.json is malformed", StatusFail
	}
	script := strings.TrimSpace(packageJSON.Scripts["test"])
	if script == "" {
		return 0, "", StatusFail
	}
	if strings.Contains(strings.ToLower(script), "no test specified") || strings.Contains(strings.ToLower(script), "error: no test") {
		return 1, script, StatusFail
	}
	return 1, script, StatusPass
}

func hasPytestConfig(r *repo.Repo) bool {
	for _, file := range []string{"pytest.ini", "tox.ini", "setup.cfg"} {
		if repo.HasFile(r.Files, file) {
			return true
		}
	}
	if !repo.HasFile(r.Files, "pyproject.toml") {
		return false
	}
	contents, err := r.Read("pyproject.toml")
	return err == nil && strings.Contains(string(contents), "[tool.pytest")
}

func hasPythonTestFile(r *repo.Repo) bool {
	for _, file := range r.Files {
		base := filepath.Base(file)
		if strings.HasSuffix(base, ".py") && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) {
			return true
		}
	}
	return false
}
