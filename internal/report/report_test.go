package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/checks"
	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

func TestRenderJSONUsesStableStringStatuses(t *testing.T) {
	minimum := 7
	repository := &repo.Repo{
		Root:        "/tmp/rebuildable",
		DisplayName: "fixture",
		Ecosystems:  []repo.Ecosystem{repo.EcosystemGo},
	}
	results := []checks.Result{
		{CheckID: "1", Status: checks.StatusPass, Findings: []checks.Finding{}},
		{CheckID: "2", Status: checks.StatusFail, Findings: []checks.Finding{{File: "package.json", Message: "lockfile is missing", Hint: "run npm install"}}},
	}

	var output bytes.Buffer
	if err := RenderJSON(&output, repository, results, &minimum); err != nil {
		t.Fatalf("render JSON: %v", err)
	}
	var document Document
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, output.String())
	}
	if document.Score != 1 || document.Total != 2 || document.MinimumScore == nil || *document.MinimumScore != 7 {
		t.Fatalf("document summary = %+v", document)
	}
	if document.Checks[0].Status != "pass" || document.Checks[1].Status != "fail" {
		t.Fatalf("statuses = %+v, want string statuses", document.Checks)
	}
	if !strings.Contains(output.String(), "\"status\": \"fail\"") {
		t.Fatalf("JSON did not contain string fail status:\n%s", output.String())
	}
}

func TestRenderMarkdownEscapesTableContent(t *testing.T) {
	repository := &repo.Repo{DisplayName: "fixture", Ecosystems: []repo.Ecosystem{repo.EcosystemNode}}
	results := []checks.Result{{
		CheckID: "5",
		Status:  checks.StatusFail,
		Findings: []checks.Finding{{
			File:    "src/a|b.js",
			Line:    4,
			Message: "test | command\nmissing",
			Hint:    "add `npm test`",
		}},
	}}

	var output bytes.Buffer
	if err := RenderMarkdown(&output, repository, results, nil); err != nil {
		t.Fatalf("render Markdown: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"# Rebuildable report", "**fail**", "src/a\\|b.js:4", "test \\| command missing", "add `npm test`"} {
		if !strings.Contains(text, expected) {
			t.Errorf("Markdown does not contain %q:\n%s", expected, text)
		}
	}
}

func TestRenderMarkdownFencesCodeSpansContainingBackticks(t *testing.T) {
	repository := &repo.Repo{DisplayName: "fix`ture", Ecosystems: []repo.Ecosystem{repo.EcosystemGo}}
	results := []checks.Result{{
		CheckID:  "2",
		Status:   checks.StatusFail,
		Findings: []checks.Finding{{File: "`odd``name`", Message: "lockfile is missing"}},
	}}

	var output bytes.Buffer
	if err := RenderMarkdown(&output, repository, results, nil); err != nil {
		t.Fatalf("render Markdown: %v", err)
	}
	text := output.String()
	for _, expected := range []string{"- Repository: ``fix`ture``", "| ``` `odd``name` ``` — lockfile is missing |"} {
		if !strings.Contains(text, expected) {
			t.Errorf("Markdown does not contain %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "\\`") {
		t.Errorf("Markdown contains a backslash-escaped backtick, which CommonMark ignores inside code spans:\n%s", text)
	}
}
