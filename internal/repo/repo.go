package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const maxScannedFileSize int64 = 1 << 20

// Repo is the read-only snapshot used by the audit checks.
type Repo struct {
	Root         string
	GitRoot      string
	DisplayName  string
	Files        []string
	TrackedFiles []string
	Ecosystems   []Ecosystem
	Read         func(path string) ([]byte, error)
	GitAvailable bool
	CommitCount  int
}

// Load reads a local repository or repository subdirectory. Git metadata is
// optional so the Git check can report a useful failure instead of making the
// entire audit a tool error.
func Load(ctx context.Context, input string) (*Repo, func(), error) {
	if isGitURL(input) {
		return loadURL(ctx, input)
	}

	root, err := filepath.Abs(input)
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve repository path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, func() {}, fmt.Errorf("stat repository path: %w", err)
	}
	if !info.IsDir() {
		return nil, func() {}, fmt.Errorf("repository path is not a directory: %s", input)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, func() {}, fmt.Errorf("resolve repository path: %w", err)
	}

	return loadRoot(ctx, root, filepath.ToSlash(input), func() {})
}

func loadURL(ctx context.Context, input string) (*Repo, func(), error) {
	temporaryRoot, err := os.MkdirTemp("", "rebuildable-clone-")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create clone directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(temporaryRoot) }

	command := exec.CommandContext(ctx, "git", "clone", "--depth=3", "--no-tags", "--", input, temporaryRoot)
	output, err := command.CombinedOutput()
	if err != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, func() {}, ctx.Err()
		}
		message := strings.TrimSpace(string(output))
		message = strings.ReplaceAll(message, input, redactInput(input))
		return nil, func() {}, fmt.Errorf("git clone failed: %s", message)
	}

	loaded, _, err := loadRoot(ctx, temporaryRoot, redactInput(input), cleanup)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return loaded, cleanup, nil
}

func loadRoot(ctx context.Context, root, displayName string, cleanup func()) (*Repo, func(), error) {
	gitRoot, gitAvailable, err := findGitRoot(ctx, root)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}

	files := make([]string, 0)
	trackedFiles := make([]string, 0)
	commitCount := 0
	if gitAvailable {
		trackedFiles, err = listTrackedFiles(ctx, gitRoot, root)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		files = append(files, trackedFiles...)
		commitCount, err = gitCommitCount(ctx, gitRoot)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
	} else {
		files, err = listWorkspaceFiles(root)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
	}

	loaded := &Repo{
		Root:         root,
		GitRoot:      gitRoot,
		DisplayName:  displayName,
		Files:        files,
		TrackedFiles: trackedFiles,
		Read:         readFile(root),
		GitAvailable: gitAvailable,
		CommitCount:  commitCount,
	}
	loaded.Ecosystems = DetectEcosystems(loaded.Files)

	return loaded, cleanup, nil
}

func isGitURL(input string) bool {
	if strings.HasPrefix(input, "git@") {
		return true
	}
	parts := strings.SplitN(input, "://", 2)
	if len(parts) != 2 {
		return false
	}
	switch strings.ToLower(parts[0]) {
	case "file", "git", "http", "https", "ssh":
		return true
	default:
		return false
	}
}

func redactInput(input string) string {
	if strings.Contains(input, "://") {
		parts := strings.SplitN(input, "://", 2)
		authorityAndPath := parts[1]
		if at := strings.LastIndex(authorityAndPath, "@"); at >= 0 {
			authorityAndPath = "<credentials-redacted>" + authorityAndPath[at:]
		}
		if query := strings.IndexAny(authorityAndPath, "?#"); query >= 0 {
			authorityAndPath = authorityAndPath[:query]
		}
		return parts[0] + "://" + authorityAndPath
	}
	if strings.HasPrefix(input, "git@") {
		if colon := strings.Index(input, ":"); colon >= 0 {
			return "git@<credentials-redacted>" + input[colon:]
		}
	}
	return input
}

func findGitRoot(ctx context.Context, root string) (string, bool, error) {
	output, err := runGit(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", false, err
		}
		return root, false, nil
	}

	gitRoot := strings.TrimSpace(string(output))
	if gitRoot == "" {
		return root, false, nil
	}
	gitRoot, err = filepath.Abs(gitRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolve Git root: %w", err)
	}
	return gitRoot, true, nil
}

func listTrackedFiles(ctx context.Context, gitRoot, scopeRoot string) ([]string, error) {
	args := []string{"ls-files", "-z", "--"}
	relativeScope, err := filepath.Rel(gitRoot, scopeRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository scope: %w", err)
	}
	if relativeScope != "." {
		args = append(args, filepath.ToSlash(relativeScope))
	}

	output, err := runGit(ctx, gitRoot, args...)
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	prefix := ""
	if relativeScope != "." {
		prefix = filepath.ToSlash(relativeScope) + "/"
	}
	files := make([]string, 0)
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		name := filepath.ToSlash(string(raw))
		if prefix != "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			name = strings.TrimPrefix(name, prefix)
		}
		fullPath := filepath.Join(scopeRoot, filepath.FromSlash(name))
		if pathHasNoSymlinkComponents(scopeRoot, fullPath) && keepScannableFile(fullPath) {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

func keepScannableFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxScannedFileSize {
		return false
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return !bytes.Contains(contents, []byte{0})
}

func pathHasNoSymlinkComponents(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func gitCommitCount(ctx context.Context, gitRoot string) (int, error) {
	output, err := runGit(ctx, gitRoot, "rev-list", "--count", "HEAD")
	if err != nil {
		if strings.Contains(string(output), "does not have any commits yet") {
			return 0, nil
		}
		// An empty repository returns a non-zero status with the diagnostic on
		// stderr, which runGit includes in the returned error.
		if strings.Contains(err.Error(), "does not have any commits yet") || strings.Contains(err.Error(), "unknown revision") {
			return 0, nil
		}
		return 0, err
	}

	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &count); err != nil {
		return 0, fmt.Errorf("parse Git commit count: %w", err)
	}
	return count, nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return output, ctx.Err()
		}
		return output, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func readFile(root string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		clean := filepath.Clean(filepath.FromSlash(path))
		full := filepath.Join(root, clean)
		relative, err := filepath.Rel(root, full)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("path escapes repository root: %s", path)
		}
		if !pathHasNoSymlinkComponents(root, full) {
			return nil, fmt.Errorf("path contains a symlink or escapes repository root: %s", path)
		}
		return os.ReadFile(full)
	}
}
