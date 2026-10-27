package repo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/testutil"
)

func TestLoadFixture(t *testing.T) {
	root := testutil.CopyFixture(t, "good-go")
	testutil.InitGitRepo(t, root, 3)

	loaded, cleanup, err := repo.Load(t.Context(), root)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	defer cleanup()

	if !loaded.GitAvailable {
		t.Fatal("expected Git repository")
	}
	if loaded.CommitCount != 3 {
		t.Fatalf("commit count = %d, want 3", loaded.CommitCount)
	}
	if len(loaded.Ecosystems) != 1 || loaded.Ecosystems[0] != repo.EcosystemGo {
		t.Fatalf("ecosystems = %v, want [go]", loaded.Ecosystems)
	}
	for _, name := range []string{"go.mod", "go.sum", "main.go"} {
		if !repo.HasFile(loaded.Files, name) {
			t.Fatalf("tracked files %v do not contain %q", loaded.Files, name)
		}
	}
}

func TestLoadScopesTrackedFilesToRequestedSubdirectory(t *testing.T) {
	parent := t.TempDir()
	fixture := testutil.CopyFixture(t, "good-go")
	scope := filepath.Join(parent, "nested")
	if err := os.Rename(fixture, scope); err != nil {
		t.Fatalf("move fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	testutil.InitGitRepo(t, parent, 3)

	loaded, cleanup, err := repo.Load(t.Context(), scope)
	if err != nil {
		t.Fatalf("load scoped fixture: %v", err)
	}
	defer cleanup()

	if loaded.Root != scope {
		t.Fatalf("root = %q, want %q", loaded.Root, scope)
	}
	if repo.HasFile(loaded.Files, "outside.txt") {
		t.Fatalf("scope leaked outside file: %v", loaded.Files)
	}
	if !repo.HasFile(loaded.Files, "go.mod") {
		t.Fatalf("scoped files = %v, want go.mod", loaded.Files)
	}
}

func TestLoadSkipsBinaryAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), make([]byte, (1<<20)+1), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.InitGitRepo(t, root, 1)

	loaded, cleanup, err := repo.Load(t.Context(), root)
	if err != nil {
		t.Fatalf("load repository: %v", err)
	}
	defer cleanup()

	if !repo.HasFile(loaded.Files, "go.mod") {
		t.Fatalf("scannable files = %v, want go.mod", loaded.Files)
	}
	for _, excluded := range []string{"binary.dat", "large.txt"} {
		if repo.HasFile(loaded.Files, excluded) {
			t.Fatalf("scannable files unexpectedly contain %q: %v", excluded, loaded.Files)
		}
	}
}

func TestLoadNonGitDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, cleanup, err := repo.Load(t.Context(), root)
	if err != nil {
		t.Fatalf("load non-Git directory: %v", err)
	}
	defer cleanup()
	if loaded.GitAvailable {
		t.Fatal("expected Git to be unavailable")
	}
	if len(loaded.Files) != 1 || loaded.Files[0] != "go.mod" {
		t.Fatalf("files = %v, want present working-tree go.mod", loaded.Files)
	}
}

func TestLoadShallowFileURLAndCleansClone(t *testing.T) {
	source := testutil.CopyFixture(t, "good-go")
	testutil.InitGitRepo(t, source, 3)
	bare := testutil.CreateBareClone(t, source)

	loaded, cleanup, err := repo.Load(t.Context(), "file://"+bare)
	if err != nil {
		t.Fatalf("load file URL: %v", err)
	}
	cloneRoot := loaded.Root
	if loaded.CommitCount != 3 {
		t.Fatalf("shallow clone commit count = %d, want 3", loaded.CommitCount)
	}
	if loaded.DisplayName != "file://"+bare {
		t.Fatalf("display name = %q, want file URL", loaded.DisplayName)
	}
	if !repo.HasFile(loaded.Files, "go.mod") {
		t.Fatalf("cloned tracked files = %v, want go.mod", loaded.Files)
	}
	if _, err := os.Stat(cloneRoot); err != nil {
		t.Fatalf("clone root before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(cloneRoot); !os.IsNotExist(err) {
		t.Fatalf("clone root after cleanup: %v", err)
	}
}

func TestReadRejectsPathEscape(t *testing.T) {
	root := testutil.CopyFixture(t, "good-go")
	testutil.InitGitRepo(t, root, 1)
	loaded, cleanup, err := repo.Load(t.Context(), root)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	defer cleanup()

	if _, err := loaded.Read("../outside"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("Read escape error = %v", err)
	}
}

func TestLoadSkipsTrackedPathsThroughSymlinkedDirectories(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "main.go"), []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.InitGitRepo(t, root, 1)
	if err := os.RemoveAll(filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}

	loaded, cleanup, err := repo.Load(t.Context(), root)
	if err != nil {
		t.Fatalf("load symlinked repository: %v", err)
	}
	defer cleanup()
	for _, file := range loaded.Files {
		if file == "src/main.go" {
			t.Fatalf("tracked file through symlink was scanned: %v", loaded.Files)
		}
	}
	if _, err := loaded.Read("src/main.go"); err == nil {
		t.Fatal("Read followed a symlinked directory outside the repository")
	}
}

func TestLoadWorkspaceSkipsUnreadableDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read directories regardless of permissions")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "data")
	if err := os.Mkdir(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "secret.txt"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	workspace, cleanup, err := repo.LoadWorkspace(t.Context(), root)
	if err != nil {
		t.Fatalf("load workspace with unreadable directory: %v", err)
	}
	defer cleanup()
	if !workspace.HasFile("go.mod") {
		t.Fatalf("workspace files = %v, want go.mod", workspace.Files)
	}
	if workspace.HasFile("data/secret.txt") {
		t.Fatalf("workspace listed a file inside an unreadable directory: %v", workspace.Files)
	}
}

func TestDetectEcosystems(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  []repo.Ecosystem
	}{
		{name: "go", files: []string{"go.mod"}, want: []repo.Ecosystem{repo.EcosystemGo}},
		{name: "node", files: []string{"package.json", "package-lock.json"}, want: []repo.Ecosystem{repo.EcosystemNode}},
		{name: "python", files: []string{"requirements.txt"}, want: []repo.Ecosystem{repo.EcosystemPython}},
		{name: "multiple", files: []string{"go.mod", "package.json", "pyproject.toml"}, want: []repo.Ecosystem{repo.EcosystemGo, repo.EcosystemNode, repo.EcosystemPython}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := repo.DetectEcosystems(test.files)
			if strings.Join(ecosystemNames(got), ",") != strings.Join(ecosystemNames(test.want), ",") {
				t.Fatalf("ecosystems = %v, want %v", got, test.want)
			}
		})
	}
}

func ecosystemNames(ecosystems []repo.Ecosystem) []string {
	names := make([]string, 0, len(ecosystems))
	for _, ecosystem := range ecosystems {
		names = append(names, ecosystem.String())
	}
	return names
}
