package checks

import (
	"context"
	"regexp"
	"strings"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

var (
	posixMachinePathPattern   = regexp.MustCompile(`/(?:home|Users)/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._~+%-]+)*`)
	windowsMachinePathPattern = regexp.MustCompile(`(?i)(?:[A-Z]:[\\/]+Users[\\/]+[^\s"'<>]+|\\\\Users\\[^\s"'<>]+)`)
)

type absolutePathCheck struct{}

func (absolutePathCheck) ID() string   { return "8" }
func (absolutePathCheck) Name() string { return "No absolute paths" }

func (absolutePathCheck) Run(_ context.Context, r *repo.Repo) Result {
	result := Result{CheckID: "8"}

	for _, file := range r.Files {
		if isDependencyFile(file) {
			continue
		}
		contents, err := r.Read(file)
		if err != nil {
			continue
		}
		for lineNumber, line := range strings.Split(string(contents), "\n") {
			lineNumber++
			for _, match := range append(posixMachinePathPattern.FindAllStringIndex(line, -1), windowsMachinePathPattern.FindAllStringIndex(line, -1)...) {
				matchPosition := match[0]
				if isURLPath(line, matchPosition) {
					continue
				}
				if matchPosition > 0 && line[matchPosition-1] == '\\' {
					continue
				}
				result.Status = StatusFail
				result.Findings = append(result.Findings, Finding{
					File:    file,
					Line:    lineNumber,
					Message: "machine-specific absolute path detected",
					Hint:    "replace the host path with a relative path or an environment-configured location",
				})
			}
		}
	}
	if result.Status != StatusFail {
		result.Status = StatusPass
		scope := "tracked"
		if !r.GitAvailable {
			scope = "working-tree"
		}
		result.Findings = []Finding{{Message: "no home-directory absolute paths found in " + scope + " text files"}}
	}
	return result
}

func isURLPath(line string, position int) bool {
	prefix := line[:position]
	return strings.Contains(prefix, "://") && !strings.ContainsAny(prefix[strings.LastIndex(prefix, "://")+3:], " \t")
}
