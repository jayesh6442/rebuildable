package repo

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitCommitCountReturnsZeroForEmptyRepository(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	count, err := gitCommitCount(t.Context(), root)
	if err != nil {
		t.Fatalf("count commits in empty repository: %v", err)
	}
	if count != 0 {
		t.Fatalf("commit count = %d, want 0", count)
	}
}

func TestGitCommitCountReturnsContextError(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := gitCommitCount(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestGitCommitCountReturnsGitErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := gitCommitCount(t.Context(), missing); err == nil {
		t.Fatal("expected error for missing Git directory")
	}
}
