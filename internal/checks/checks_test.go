package checks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/testutil"
)

func TestGitRepoCheck(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		commits int
		want    Status
	}{
		{name: "three commits pass", fixture: "good-go", commits: 3, want: StatusPass},
		{name: "one commit warns", fixture: "good-go", commits: 1, want: StatusWarn},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadFixture(t, test.fixture, test.commits)
			result := (gitRepoCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
		})
	}

	root := testutil.CopyFixture(t, "good-go")
	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("load non-Git fixture: %v", err)
	}
	defer cleanup()
	result := (gitRepoCheck{}).Run(context.Background(), loaded)
	if result.Status != StatusFail {
		t.Fatalf("non-Git status = %s, want fail", result.Status)
	}
}

func TestLockfileCheck(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    Status
		message string
	}{
		{name: "Go checksum file", fixture: "good-go", want: StatusPass, message: "Go checksum file found"},
		{name: "Node lockfile missing", fixture: "bad-node", want: StatusFail, message: "no npm, pnpm, Yarn, or Bun lockfile found"},
		{name: "Python requirement range", fixture: "py-unpinned", want: StatusWarn, message: "not pinned with =="},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadFixture(t, test.fixture, 3)
			result := (lockfileCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
			if !containsFinding(result.Findings, test.message) {
				t.Fatalf("findings = %+v, want message containing %q", result.Findings, test.message)
			}
		})
	}
}

func TestRuntimeCheck(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    Status
		message string
	}{
		{name: "Go toolchain", fixture: "good-go", want: StatusPass, message: "toolchain go1.27.2"},
		{name: "Node missing", fixture: "bad-node", want: StatusFail, message: "Node runtime version is not pinned"},
		{name: "Python missing", fixture: "py-unpinned", want: StatusFail, message: "Python runtime version is not pinned"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadFixture(t, test.fixture, 3)
			result := (runtimeCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
			if !containsFinding(result.Findings, test.message) {
				t.Fatalf("findings = %+v, want message containing %q", result.Findings, test.message)
			}
		})
	}
}

func TestGoRuntimeRequiresExactToolchain(t *testing.T) {
	tests := []struct {
		name  string
		goMod string
		want  Status
	}{
		{name: "exact toolchain", goMod: "module example.com/runtime\ngo 1.27\ntoolchain go1.27.2\n", want: StatusPass},
		{name: "exact go and toolchain", goMod: "module example.com/runtime\ngo 1.27.1\ntoolchain go1.27.2\n", want: StatusPass},
		{name: "language version toolchain", goMod: "module example.com/runtime\ngo 1.27\ntoolchain go1.27\n", want: StatusWarn},
		{name: "default toolchain", goMod: "module example.com/runtime\ngo 1.27\ntoolchain default\n", want: StatusWarn},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadContentRepo(t, map[string]string{"go.mod": test.goMod, "go.sum": ""}, repo.EcosystemGo)
			result := (runtimeCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
			if test.want == StatusWarn && !containsFinding(result.Findings, "toolchain is not pinned") {
				t.Fatalf("findings = %+v, want toolchain is not pinned", result.Findings)
			}
		})
	}
}

func TestLockfileVariants(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		ecosystem repo.Ecosystem
		want      Status
	}{
		{name: "npm", ecosystem: repo.EcosystemNode, want: StatusPass, files: map[string]string{"package.json": "{}", "package-lock.json": "{}"}},
		{name: "pnpm", ecosystem: repo.EcosystemNode, want: StatusPass, files: map[string]string{"package.json": "{}", "pnpm-lock.yaml": "lockfileVersion: 9"}},
		{name: "uv", ecosystem: repo.EcosystemPython, want: StatusPass, files: map[string]string{"pyproject.toml": "[project]\nname = \"fixture\"\n", "uv.lock": "version = 1"}},
		{name: "pinned requirements", ecosystem: repo.EcosystemPython, want: StatusPass, files: map[string]string{"requirements.txt": "requests==2.31.0\n"}},
		{name: "pinned requirements with marker and comment", ecosystem: repo.EcosystemPython, want: StatusPass, files: map[string]string{"requirements.txt": "requests==2.31.0 ; python_version >= \"3.8\"  # http\n"}},
		{name: "range hidden by marker", ecosystem: repo.EcosystemPython, want: StatusWarn, files: map[string]string{"requirements.txt": "requests==2.31.0\nimportlib-metadata>=6; python_version == \"3.8\"\n"}},
		{name: "range hidden by comment", ecosystem: repo.EcosystemPython, want: StatusWarn, files: map[string]string{"requirements.txt": "requests>=2  # was ==2.31.0\n"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loaded := loadContentRepo(t, test.files, test.ecosystem)
			result := (lockfileCheck{}).Run(context.Background(), loaded)
			if result.Status != test.want {
				t.Fatalf("status = %s, want %s; findings = %+v", result.Status, test.want, result.Findings)
			}
		})
	}
}

func TestRegistryAndScore(t *testing.T) {
	registry := Registry()
	if len(registry) != 8 {
		t.Fatalf("registry length = %d, want 8", len(registry))
	}
	for index, check := range registry {
		wantID := string(rune('1' + index))
		if check.ID() != wantID {
			t.Errorf("check %d ID = %q, want %q", index, check.ID(), wantID)
		}
	}
	results := []Result{
		{Status: StatusPass},
		{Status: StatusWarn},
		{Status: StatusFail},
		{Status: StatusSkip},
	}
	if got := Score(results); got != 1 {
		t.Fatalf("score = %d, want 1", got)
	}
}

func loadFixture(t *testing.T, name string, commits int) *repo.Repo {
	t.Helper()
	root := testutil.CopyFixture(t, name)
	testutil.InitGitRepo(t, root, commits)
	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("load fixture %q: %v", name, err)
	}
	t.Cleanup(cleanup)
	return loaded
}

func loadContentRepo(t *testing.T, files map[string]string, ecosystem repo.Ecosystem) *repo.Repo {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	testutil.InitGitRepo(t, root, 3)
	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("load content repository: %v", err)
	}
	t.Cleanup(cleanup)
	if len(loaded.Ecosystems) == 0 {
		loaded.Ecosystems = []repo.Ecosystem{ecosystem}
	}
	return loaded
}

func containsFinding(findings []Finding, message string) bool {
	for _, finding := range findings {
		if strings.Contains(finding.Message, message) {
			return true
		}
	}
	return false
}
