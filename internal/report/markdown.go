package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/jayeshsarvaiya/rebuildable/internal/checks"
	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

func RenderMarkdown(w io.Writer, r *repo.Repo, results []checks.Result, minimumScore *int) error {
	document := BuildDocument(r, results, minimumScore)
	if _, err := fmt.Fprintf(w, "# Rebuildable report\n\n- Repository: %s\n- Ecosystems: %s\n- Score: **%d/%d**\n", codeSpan(document.Repository), codeSpan(strings.Join(document.Ecosystems, ", ")), document.Score, document.Total); err != nil {
		return err
	}
	if document.MinimumScore != nil {
		if _, err := fmt.Fprintf(w, "- Minimum score: **%d**\n", *document.MinimumScore); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "\n| # | Check | Status | Fixable |\n| ---: | --- | --- | :---: |\n"); err != nil {
		return err
	}
	for _, check := range document.Checks {
		if _, err := fmt.Fprintf(w, "| %s | %s | **%s** | %s |\n", check.ID, escapeCell(check.Name), check.Status, yesNo(check.Fixable)); err != nil {
			return err
		}
		for _, finding := range check.Findings {
			location := finding.File
			if finding.Line > 0 {
				location = fmt.Sprintf("%s:%d", location, finding.Line)
			}
			message := escapeCell(finding.Message)
			if location != "" {
				message = fmt.Sprintf("%s — %s", codeSpan(escapeCell(location)), message)
			}
			if _, err := fmt.Fprintf(w, "|  |  |  | %s |\n", message); err != nil {
				return err
			}
			if finding.Hint != "" {
				if _, err := fmt.Fprintf(w, "|  |  |  | Hint: %s |\n", escapeCell(finding.Hint)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func escapeCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

// codeSpan wraps value in a CommonMark code span. Backslash escapes are not
// processed inside code spans, so the fence must be longer than any backtick
// run in value, with space padding when value starts or ends with a backtick.
func codeSpan(value string) string {
	longest, run := 0, 0
	for _, r := range value {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") {
		value = " " + value + " "
	}
	return fence + value + fence
}
