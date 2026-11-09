package checks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/testutil"
)

func TestTrackedEnvironmentFilesAtAnyDepth(t *testing.T) {
	root := t.TempDir()
	sensitive := []string{
		".env", ".env.production", ".env.local", ".env.staging",
		"backend/.env", "apps/web/.env.local", "services/api/.env.development",
	}
	templates := []string{
		".env.example", ".env.sample", "configs/.env.template", "apps/web/.env.dist",
		"README.md", "some.env.production",
	}
	for _, name := range append(sensitive, templates...) {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatalf("create parent directory for %s: %v", name, err)
		}
		if err := os.WriteFile(filename, []byte("PORT=1234\n"), 0o644); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	testutil.InitGitRepo(t, root, 1)
	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	result := (environmentCheck{}).Run(context.Background(), loaded)
	if result.Status != StatusFail || len(result.Findings) != len(sensitive) {
		t.Fatalf("status = %s, findings = %+v; expected %d tracked env findings", result.Status, result.Findings, len(sensitive))
	}
	found := make(map[string]bool)
	for _, f := range result.Findings {
		if f.Message != "a .env file is tracked by Git" {
			t.Errorf("unexpected finding: %+v", f)
		}
		found[f.File] = true
	}
	for _, name := range sensitive {
		if !found[name] {
			t.Errorf("missing finding for %s", name)
		}
	}
	for _, name := range templates {
		if found[name] {
			t.Errorf("incorrectly flagged template/ordinary file %s", name)
		}
	}
}

func TestUntrackedEnvironmentFileIsNotFlagged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.InitGitRepo(t, root, 1)
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("PORT=1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	result := (environmentCheck{}).Run(context.Background(), loaded)
	if result.Status != StatusPass {
		t.Fatalf("status = %s, want pass; findings = %+v", result.Status, result.Findings)
	}
}
