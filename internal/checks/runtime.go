package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

var exactRuntimeVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
var goDirectiveVersion = regexp.MustCompile(`^\d+\.\d+(?:\.\d+)?$`)

type runtimeCheck struct{}

func (runtimeCheck) ID() string   { return "3" }
func (runtimeCheck) Name() string { return "Runtime version pinned" }

func (runtimeCheck) Run(_ context.Context, r *repo.Repo) Result {
	result := Result{CheckID: "3", Fixable: true}
	if len(r.Ecosystems) == 0 {
		result.Status = StatusSkip
		result.Findings = []Finding{{
			Message: "no supported ecosystem detected",
			Hint:    "declare the runtime in a supported version file",
		}}
		return result
	}

	statuses := make([]Status, 0, len(r.Ecosystems))
	for _, ecosystem := range r.Ecosystems {
		status, finding := runtimeForEcosystem(r, ecosystem)
		statuses = append(statuses, status)
		result.Findings = append(result.Findings, finding)
	}
	result.Status = combineStatuses(statuses...)
	return result
}

func runtimeForEcosystem(r *repo.Repo, ecosystem repo.Ecosystem) (Status, Finding) {
	switch ecosystem {
	case repo.EcosystemGo:
		return goRuntime(r)
	case repo.EcosystemNode:
		return nodeRuntime(r)
	case repo.EcosystemPython:
		return pythonRuntime(r)
	default:
		return StatusSkip, Finding{Message: "unsupported ecosystem"}
	}
}

func goRuntime(r *repo.Repo) (Status, Finding) {
	contents, err := readText(r, "go.mod")
	if err != nil {
		return StatusFail, Finding{File: "go.mod", Message: fmt.Sprintf("go.mod could not be read: %v", err), Hint: "add go and toolchain directives to go.mod"}
	}
	goLine, goVersion := directive(contents, "go")
	toolchainLine, toolchainVersion := directive(contents, "toolchain")
	if goVersion != "" && toolchainVersion != "" && goDirectiveVersion.MatchString(goVersion) && strings.HasPrefix(toolchainVersion, "go") && exactRuntimeVersion.MatchString(strings.TrimPrefix(toolchainVersion, "go")) {
		return StatusPass, Finding{File: "go.mod", Line: toolchainLine, Message: fmt.Sprintf("Go %s with toolchain %s", goVersion, toolchainVersion)}
	}
	if goVersion != "" {
		return StatusWarn, Finding{File: "go.mod", Line: goLine, Message: "Go language version is declared but toolchain is not pinned", Hint: "add a toolchain goX.Y.Z line to go.mod"}
	}
	return StatusFail, Finding{File: "go.mod", Message: "go and toolchain version pins are missing", Hint: "add go and toolchain directives to go.mod"}
}

func nodeRuntime(r *repo.Repo) (Status, Finding) {
	for _, file := range []string{".nvmrc", ".node-version"} {
		if !repo.HasFile(r.Files, file) {
			continue
		}
		contents, err := readText(r, file)
		if err != nil {
			return StatusFail, Finding{File: file, Message: fmt.Sprintf("%s could not be read: %v", file, err), Hint: "write an exact Node version to .nvmrc"}
		}
		version := strings.TrimSpace(contents)
		if exactRuntimeVersion.MatchString(version) {
			return StatusPass, Finding{File: file, Line: 1, Message: fmt.Sprintf("Node %s is pinned", version)}
		}
		return StatusWarn, Finding{File: file, Line: 1, Message: "Node version file is not an exact semantic version", Hint: "write an exact version such as 20.11.1"}
	}

	if repo.HasFile(r.Files, ".tool-versions") {
		if line, version := toolVersion(r, "nodejs"); version != "" {
			if exactRuntimeVersion.MatchString(version) {
				return StatusPass, Finding{File: ".tool-versions", Line: line, Message: fmt.Sprintf("Node %s is pinned", version)}
			}
			return StatusWarn, Finding{File: ".tool-versions", Line: line, Message: "Node version in .tool-versions is not exact", Hint: "pin nodejs to a full semantic version"}
		}
	}

	if repo.HasFile(r.Files, "package.json") {
		contents, err := readText(r, "package.json")
		if err == nil {
			var packageJSON struct {
				Engines struct {
					Node string `json:"node"`
				} `json:"engines"`
			}
			if json.Unmarshal([]byte(contents), &packageJSON) == nil && packageJSON.Engines.Node != "" {
				if exactRuntimeVersion.MatchString(packageJSON.Engines.Node) {
					return StatusPass, Finding{File: "package.json", Message: fmt.Sprintf("engines.node pins Node %s", packageJSON.Engines.Node)}
				}
				return StatusWarn, Finding{File: "package.json", Message: "engines.node declares a range instead of an exact version", Hint: "pin Node with .nvmrc or .node-version"}
			}
		}
	}

	return StatusFail, Finding{File: "package.json", Message: "Node runtime version is not pinned", Hint: "write an exact version such as 20.11.1 to .nvmrc"}
}

func pythonRuntime(r *repo.Repo) (Status, Finding) {
	for _, file := range []string{".python-version"} {
		if !repo.HasFile(r.Files, file) {
			continue
		}
		contents, err := readText(r, file)
		if err != nil {
			return StatusFail, Finding{File: file, Message: fmt.Sprintf("%s could not be read: %v", file, err), Hint: "write an exact Python version to .python-version"}
		}
		version := strings.TrimSpace(contents)
		if exactRuntimeVersion.MatchString(version) {
			return StatusPass, Finding{File: file, Line: 1, Message: fmt.Sprintf("Python %s is pinned", version)}
		}
		return StatusWarn, Finding{File: file, Line: 1, Message: "Python version file is not an exact semantic version", Hint: "write an exact version such as 3.12.1"}
	}

	if repo.HasFile(r.Files, ".tool-versions") {
		if line, version := toolVersion(r, "python"); version != "" {
			if exactRuntimeVersion.MatchString(version) {
				return StatusPass, Finding{File: ".tool-versions", Line: line, Message: fmt.Sprintf("Python %s is pinned", version)}
			}
			return StatusWarn, Finding{File: ".tool-versions", Line: line, Message: "Python version in .tool-versions is not exact", Hint: "pin python to a full semantic version"}
		}
	}

	if repo.HasFile(r.Files, "pyproject.toml") {
		contents, err := readText(r, "pyproject.toml")
		if err == nil {
			if line, value := directive(contents, "requires-python"); value != "" {
				if strings.HasPrefix(value, "==") && exactRuntimeVersion.MatchString(strings.TrimPrefix(value, "==")) {
					return StatusPass, Finding{File: "pyproject.toml", Line: line, Message: fmt.Sprintf("requires-python pins %s", value)}
				}
				return StatusWarn, Finding{File: "pyproject.toml", Line: line, Message: "requires-python declares a range instead of an exact version", Hint: "write an exact version to .python-version"}
			}
		}
	}

	return StatusFail, Finding{File: "pyproject.toml", Message: "Python runtime version is not pinned", Hint: "write an exact version such as 3.12.1 to .python-version"}
}

func readText(r *repo.Repo, file string) (string, error) {
	contents, err := r.Read(file)
	if err != nil {
		return "", err
	}
	return string(contents), nil
}

func directive(contents, name string) (lineNumber int, value string) {
	for index, rawLine := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"=") {
			value = strings.TrimSpace(strings.TrimPrefix(line, name))
			value = strings.TrimSpace(strings.TrimPrefix(value, "="))
			value = strings.TrimSpace(strings.Trim(value, "\"'"))
			return index + 1, value
		}
	}
	return 0, ""
}

func toolVersion(r *repo.Repo, name string) (lineNumber int, version string) {
	contents, err := readText(r, ".tool-versions")
	if err != nil {
		return 0, ""
	}
	for index, rawLine := range strings.Split(contents, "\n") {
		fields := strings.Fields(rawLine)
		if len(fields) >= 2 && fields[0] == name {
			return index + 1, fields[1]
		}
	}
	return 0, ""
}
