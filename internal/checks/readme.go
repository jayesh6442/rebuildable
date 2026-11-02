package checks

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

type readmeCheck struct{}

func (readmeCheck) ID() string   { return "6" }
func (readmeCheck) Name() string { return "README with setup, run, test" }

func (readmeCheck) Run(_ context.Context, r *repo.Repo) Result {
	result := Result{CheckID: "6", Fixable: true}
	if !repo.HasFile(r.Files, "README.md") {
		result.Status = StatusFail
		result.Findings = []Finding{{
			File:    "README.md",
			Message: "README.md is missing",
			Hint:    "add Setup, Run, and Test sections to README.md",
		}}
		return result
	}
	contents, err := r.Read("README.md")
	if err != nil {
		result.Status = StatusFail
		result.Findings = []Finding{{File: "README.md", Message: fmt.Sprintf("README.md could not be read: %v", err), Hint: "add a readable README.md"}}
		return result
	}

	found := readmeSections(string(contents))
	missing := make([]string, 0, 3)
	for _, section := range []string{"setup", "run", "test"} {
		if !found[section] {
			missing = append(missing, section)
		}
	}
	if len(missing) == 0 {
		result.Status = StatusPass
		result.Findings = []Finding{{File: "README.md", Message: "setup, run, and test sections found"}}
		return result
	}
	result.Findings = []Finding{{
		File:    "README.md",
		Message: fmt.Sprintf("README is missing %s section(s)", strings.Join(missing, ", ")),
		Hint:    "add headings for Setup, Run, and Test with the commands a new contributor should use",
	}}
	if len(missing) == 3 {
		result.Status = StatusFail
	} else {
		result.Status = StatusWarn
	}
	return result
}

func readmeSections(contents string) map[string]bool {
	sections := map[string]bool{"setup": false, "run": false, "test": false}
	inFence := false
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#")))
		// CommonMark allows an optional closing sequence of #s preceded by a space.
		if closed := strings.TrimRight(title, "#"); closed == "" || strings.HasSuffix(closed, " ") || strings.HasSuffix(closed, "\t") {
			title = closed
		}
		title = strings.Trim(title, " :.-")
		switch {
		case title == "setup" || title == "install" || title == "installation" || title == "getting started":
			sections["setup"] = true
		case title == "run" || title == "usage" || title == "running":
			sections["run"] = true
		case title == "test" || title == "tests" || title == "testing":
			sections["test"] = true
		}
	}
	return sections
}

func ReadmeSections(contents string) map[string]bool {
	return readmeSections(contents)
}
