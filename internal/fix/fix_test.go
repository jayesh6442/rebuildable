package fix

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/checks"
	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/testutil"
)

func TestDryRunProducesDiffWithoutMutation(t *testing.T) {
	root := testutil.CopyFixture(t, "demo-node")
	testutil.InitGitRepo(t, root, 3)
	before, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{})
	if err != nil {
		t.Fatalf("plan changes: %v", err)
	}
	var diff bytes.Buffer
	if err := RenderUnifiedDiff(&diff, plan); err != nil {
		t.Fatalf("render diff: %v", err)
	}
	if !strings.Contains(diff.String(), "+++ b/Dockerfile") || !strings.Contains(diff.String(), "+++ b/.nvmrc") {
		t.Fatalf("diff = %s", diff.String())
	}
	after, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("dry-run changed an existing file")
	}
	if _, err := os.Stat(filepath.Join(root, "Dockerfile")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created Dockerfile: %v", err)
	}
}

func TestWriteRaisesAuditScoreAndIsIdempotent(t *testing.T) {
	root := testutil.CopyFixture(t, "demo-node")
	testutil.InitGitRepo(t, root, 3)
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{})
	if err != nil {
		t.Fatalf("plan changes: %v", err)
	}
	result, err := ApplyChanges(context.Background(), workspace, plan, false)
	if err != nil {
		t.Fatalf("apply changes: %v", err)
	}
	if len(result.Applied) == 0 || len(result.Conflicts) != 0 {
		t.Fatalf("apply result = %+v", result)
	}
	for _, path := range []string{"Dockerfile", ".dockerignore", ".nvmrc", "README.md", ".env.example"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("generated %s: %v", path, err)
		}
	}
	gitAdd(t, root)
	loaded, cleanup, err := repo.Load(context.Background(), root)
	if err != nil {
		t.Fatalf("reload fixed repository: %v", err)
	}
	t.Cleanup(cleanup)
	score := checks.Score(checks.RunAll(context.Background(), loaded))
	if score <= 3 {
		t.Fatalf("fixed score = %d, want greater than 3", score)
	}

	workspace = loadWorkspace(t, root)
	plan, err = PlanChanges(context.Background(), workspace, Options{})
	if err != nil {
		t.Fatalf("replan changes: %v", err)
	}
	for _, change := range plan.Changes {
		if change.Action != ActionNoop {
			t.Fatalf("second plan is not idempotent: %+v", plan.Changes)
		}
	}
}

func TestOnlyLimitsGenerators(t *testing.T) {
	root := testutil.CopyFixture(t, "bad-node")
	testutil.InitGitRepo(t, root, 3)
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"pins"}})
	if err != nil {
		t.Fatalf("plan pins: %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != ".nvmrc" {
		t.Fatalf("pin-only changes = %+v", plan.Changes)
	}
	if _, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"unknown"}}); err == nil {
		t.Fatal("unknown generator unexpectedly succeeded")
	}
}

func TestExistingFilesRequireForce(t *testing.T) {
	root := testutil.CopyFixture(t, "bad-node")
	testutil.InitGitRepo(t, root, 3)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"readme"}})
	if err != nil {
		t.Fatalf("plan README: %v", err)
	}
	result, err := ApplyChanges(context.Background(), workspace, plan, false)
	if err != nil {
		t.Fatalf("apply protected README: %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want one conflict", result.Conflicts)
	}
	contents, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "# Existing\n" {
		t.Fatalf("protected README changed: %s", contents)
	}
}

func TestGoDockerfileUsesSingleDetectedCommandPackage(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/service\ngo 1.27\ntoolchain go1.27.2\n")
	writeTestFile(t, root, "go.sum", "")
	writeTestFile(t, root, "cmd/service/main.go", "package main\nfunc main() {}\n")
	testutil.InitGitRepo(t, root, 3)
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"dockerfile"}})
	if err != nil {
		t.Fatalf("plan Go Dockerfile: %v", err)
	}
	var dockerfile string
	for _, change := range plan.Changes {
		if change.Path == "Dockerfile" {
			dockerfile = string(change.Content)
		}
	}
	if !strings.Contains(dockerfile, "go build -trimpath -o /out/app ./cmd/service") || !strings.Contains(dockerfile, "rebuildable-test") {
		t.Fatalf("generated Dockerfile = %s", dockerfile)
	}
}

func TestEnvironmentGeneratorMergesExistingExample(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/env\ngo 1.27\ntoolchain go1.27.2\n")
	writeTestFile(t, root, "go.sum", "")
	writeTestFile(t, root, "main.go", "package main\nfunc load() { _ = os.Getenv(\"NEW_KEY\") }\n")
	writeTestFile(t, root, ".env.example", "# Keep this comment\nOLD_KEY=example\n")
	testutil.InitGitRepo(t, root, 3)
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"env"}})
	if err != nil {
		t.Fatalf("plan env example: %v", err)
	}
	if len(plan.Changes) != 1 || !strings.Contains(string(plan.Changes[0].Content), "OLD_KEY=example") || !strings.Contains(string(plan.Changes[0].Content), "NEW_KEY=") {
		t.Fatalf("merged env example = %+v", plan.Changes)
	}
}

func TestPinsReplacesInexactGoToolchainDirective(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/pins\n\ngo 1.27.1\n\ntoolchain default\n")
	writeTestFile(t, root, "go.sum", "")
	writeTestFile(t, root, "main.go", "package main\nfunc main() {}\n")
	testutil.InitGitRepo(t, root, 3)
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"pins"}})
	if err != nil {
		t.Fatalf("plan Go pins: %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != "go.mod" {
		t.Fatalf("pin changes = %+v", plan.Changes)
	}
	want := "module example.com/pins\n\ngo 1.27.1\n\ntoolchain go1.27.1\n"
	if got := string(plan.Changes[0].Content); got != want {
		t.Fatalf("generated go.mod = %q, want %q", got, want)
	}
}

func TestPinsReplacesLanguageVersionGoToolchain(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/pins\n\ngo 1.27\n\ntoolchain go1.27\n")
	writeTestFile(t, root, "go.sum", "")
	writeTestFile(t, root, "main.go", "package main\nfunc main() {}\n")
	testutil.InitGitRepo(t, root, 3)
	workspace := loadWorkspace(t, root)
	plan, err := PlanChanges(context.Background(), workspace, Options{Only: []string{"pins"}})
	if err != nil {
		t.Fatalf("plan Go pins: %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != "go.mod" {
		t.Fatalf("pin changes = %+v", plan.Changes)
	}
	want := "module example.com/pins\n\ngo 1.27\n\ntoolchain go" + defaultGoVersion + "\n"
	if got := string(plan.Changes[0].Content); got != want {
		t.Fatalf("generated go.mod = %q, want %q", got, want)
	}
}

func loadWorkspace(t *testing.T, root string) *repo.Workspace {
	t.Helper()
	workspace, cleanup, err := repo.LoadWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	t.Cleanup(cleanup)
	return workspace
}

func gitAdd(t *testing.T, root string) {
	t.Helper()
	command := exec.Command("git", "add", ".")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, output)
	}
}

func writeTestFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
