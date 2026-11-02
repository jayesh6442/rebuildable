package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/testutil"
)

const failingCleanupDocker = `#!/bin/sh
case "$1" in
  create) echo fake-container-id ;;
  inspect) echo 'false|0' ;;
  rm|rmi) echo "permission denied" ; exit 1 ;;
esac
exit 0
`

func TestRunReturnsStagesWhenDockerCleanupFails(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(failingCleanupDocker), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := testutil.CopyFixture(t, "good-go")
	testutil.InitGitRepo(t, root, 3)
	workspace, cleanup, err := repo.LoadWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	defer cleanup()

	report, err := Run(context.Background(), workspace, Options{Timeout: time.Minute, Port: 18080, Trusted: true})
	if !errors.Is(err, ErrCleanupFailed) {
		t.Fatalf("error = %v, want ErrCleanupFailed", err)
	}
	if !report.Passed() || len(report.Stages) != 4 {
		t.Fatalf("stages were not returned with cleanup error: %+v", report.Stages)
	}
	if len(report.Cleanup) == 0 {
		t.Fatalf("cleanup failures missing from report: %+v", report)
	}
}
