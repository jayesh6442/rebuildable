package checks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/testutil"
)

func TestDockerfileCheck(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  Status
	}{
		{name: "missing", files: map[string]string{"go.mod": "module example.com/test\n"}, want: StatusFail},
		{name: "missing ignore warns", files: map[string]string{"Dockerfile": "FROM scratch\nENTRYPOINT [\"/bin/app\"]\n"}, want: StatusWarn},
		{name: "complete", files: map[string]string{"Dockerfile": "# comment\nFROM scratch\nCMD [\"app\"]\n", ".dockerignore": ".git\n"}, want: StatusPass},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadContentRepo(t, test.files, repo.EcosystemGo)
			result := (dockerfileCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
		})
	}
}

func TestTestCommandCheck(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  Status
	}{
		{name: "make target", files: map[string]string{"Makefile": ".PHONY: test\ntest:\n\tgo test ./...\n"}, want: StatusPass},
		{name: "placeholder rejected", files: map[string]string{"package.json": "{\"scripts\":{\"test\":\"echo Error: no test specified && exit 1\"}}\n"}, want: StatusFail},
		{name: "Go test file", files: map[string]string{"go.mod": "module example.com/test\n", "sample_test.go": "package sample\n"}, want: StatusPass},
		{name: "missing", files: map[string]string{"package.json": "{}\n"}, want: StatusFail},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadContentRepo(t, test.files, repo.EcosystemGo)
			result := (testCommandCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
		})
	}
}

func TestReadmeCheckIgnoresFencedHeadings(t *testing.T) {
	partial := loadContentRepo(t, map[string]string{
		"README.md": "# Project\n\n## Setup\n\nInstall Go.\n\n```markdown\n## Run\n## Test\n```\n",
	}, repo.EcosystemGo)
	result := (readmeCheck{}).Run(context.Background(), partial)
	if result.Status != StatusWarn {
		t.Fatalf("status = %s, want warn; findings = %+v", result.Status, result.Findings)
	}

	complete := loadContentRepo(t, map[string]string{
		"README.md": "## Install\n\nInstall dependencies.\n\n### Usage\n\nRun the app.\n\n## Testing\n\nRun tests.\n",
	}, repo.EcosystemGo)
	result = (readmeCheck{}).Run(context.Background(), complete)
	if result.Status != StatusPass {
		t.Fatalf("complete status = %s, want pass; findings = %+v", result.Status, result.Findings)
	}
}

func TestReadmeCheckAcceptsClosingHashes(t *testing.T) {
	loaded := loadContentRepo(t, map[string]string{
		"README.md": "# Project #\n\n## Setup ##\n\nInstall Go.\n\n## Run ####\n\nRun the app.\n\n### Test ###   \n\nRun tests.\n",
	}, repo.EcosystemGo)
	result := (readmeCheck{}).Run(context.Background(), loaded)
	if result.Status != StatusPass {
		t.Fatalf("status = %s, want pass; findings = %+v", result.Status, result.Findings)
	}

	attached := loadContentRepo(t, map[string]string{
		"README.md": "## Setup#\n\n## Run\n\n## Test\n",
	}, repo.EcosystemGo)
	result = (readmeCheck{}).Run(context.Background(), attached)
	if result.Status != StatusWarn {
		t.Fatalf("attached hash status = %s, want warn; findings = %+v", result.Status, result.Findings)
	}
}

func TestLeakyFixtureGeneratedAtRuntime(t *testing.T) {
	root := testutil.CopyFixture(t, "good-go")
	secret := "AK" + "IA" + strings.Repeat("Z", 16)
	machinePath := string(filepath.Separator) + strings.Join([]string{"home", "reviewer", "data.csv"}, string(filepath.Separator))
	goName := "MISSING_GO"
	nodeName := "MISSING_NODE"
	pythonName := "MISSING_PY"
	goCall := "os." + "Getenv" + "(\"" + goName + "\")"
	nodeCall := "process" + ".env." + nodeName
	pythonCall := "os." + "environ[\"" + pythonName + "\"]"

	writeFile(t, root, "main.go", fmt.Sprintf("package main\nfunc load() { _ = %s; _ = \"%s\"; _ = \"%s\" }\n", goCall, secret, machinePath))
	writeFile(t, root, "app.js", "const value = "+nodeCall+";\n")
	writeFile(t, root, "app.py", "value = "+pythonCall+"\n")
	writeFile(t, root, ".env.example", "PRESENT_KEY=\n")
	writeFile(t, root, ".gitignore", ".env\nignored.js\n")
	writeFile(t, root, ".env", "TOKEN="+secret+"\n")
	writeFile(t, root, "ignored.js", "const ignored = \""+machinePath+"\";\n")
	testutil.InitGitRepo(t, root, 3)

	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("load generated fixture: %v", err)
	}
	defer cleanup()

	environmentResult := (environmentCheck{}).Run(context.Background(), loaded)
	if environmentResult.Status != StatusFail {
		t.Fatalf("environment status = %s, want fail; findings = %+v", environmentResult.Status, environmentResult.Findings)
	}
	for _, finding := range environmentResult.Findings {
		if strings.Contains(finding.Message, secret) || strings.Contains(finding.Message, machinePath) {
			t.Fatalf("finding leaked sensitive value: %+v", finding)
		}
		if finding.File == ".env" || finding.File == "ignored.js" {
			t.Fatalf("untracked file was scanned: %+v", finding)
		}
	}
	for _, name := range []string{goName, nodeName, pythonName} {
		if !containsFinding(environmentResult.Findings, name) {
			t.Errorf("environment findings do not mention %s: %+v", name, environmentResult.Findings)
		}
	}

	pathResult := (absolutePathCheck{}).Run(context.Background(), loaded)
	if pathResult.Status != StatusFail {
		t.Fatalf("path status = %s, want fail; findings = %+v", pathResult.Status, pathResult.Findings)
	}
	if len(pathResult.Findings) == 0 || pathResult.Findings[0].File != "main.go" {
		t.Fatalf("path findings = %+v, want tracked main.go", pathResult.Findings)
	}
}

func TestAbsolutePathCheckAllowsURLsAndContainerPaths(t *testing.T) {
	loaded := loadContentRepo(t, map[string]string{
		"notes.txt": "See https://example.invalid/Users/reviewer and use /app inside the container.\n",
	}, repo.EcosystemGo)
	result := (absolutePathCheck{}).Run(context.Background(), loaded)
	if result.Status != StatusPass {
		t.Fatalf("status = %s, want pass; findings = %+v", result.Status, result.Findings)
	}

	windowsPath := "C:" + string(filepath.Separator) + strings.Join([]string{"Users", "reviewer", "data.csv"}, string(filepath.Separator))
	loaded = loadContentRepo(t, map[string]string{"config.txt": windowsPath + "\n"}, repo.EcosystemGo)
	result = (absolutePathCheck{}).Run(context.Background(), loaded)
	if result.Status != StatusFail {
		t.Fatalf("Windows path status = %s, want fail; findings = %+v", result.Status, result.Findings)
	}
}

func writeFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
