package repo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Workspace represents the writable working tree used by fix and verify.
// Files includes present regular files even when they are not tracked yet;
// TrackedFiles preserves the audit-only Git snapshot.
type Workspace struct {
	Root         string
	Files        []string
	TrackedFiles []string
	Ecosystems   []Ecosystem
	Read         func(path string) ([]byte, error)
	GitAvailable bool
}

func LoadWorkspace(ctx context.Context, input string) (*Workspace, func(), error) {
	if isGitURL(input) {
		return nil, func() {}, fmt.Errorf("fix and verify require a local path, not a Git URL")
	}
	loaded, cleanup, err := Load(ctx, input)
	if err != nil {
		return nil, func() {}, err
	}

	files, err := listWorkspaceFiles(loaded.Root)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	workspace := &Workspace{
		Root:         loaded.Root,
		Files:        files,
		TrackedFiles: append([]string(nil), loaded.Files...),
		Ecosystems:   DetectEcosystems(files),
		Read:         readFile(loaded.Root),
		GitAvailable: loaded.GitAvailable,
	}
	return workspace, cleanup, nil
}

func listWorkspaceFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable entries below the root (for example root-owned
			// container volumes) are skipped like unreadable files instead of
			// aborting the whole listing.
			if path != root && errors.Is(err, fs.ErrPermission) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err
		}
		if path != root && ignoredWorkspaceDirectory(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if keepScannableFile(path) {
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list workspace files: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func ignoredWorkspaceDirectory(name string) bool {
	switch name {
	case ".git", ".rebuildable", "node_modules", "dist", "bin", "build", "__pycache__", ".venv", "venv":
		return true
	default:
		return false
	}
}

func (w *Workspace) HasFile(name string) bool {
	for _, file := range w.Files {
		if file == name {
			return true
		}
	}
	return false
}

func (w *Workspace) IsTracked(name string) bool {
	for _, file := range w.TrackedFiles {
		if file == name {
			return true
		}
	}
	return false
}

func (w *Workspace) String() string {
	return strings.TrimSpace(w.Root)
}
