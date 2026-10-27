package verify

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
)

func TestTestCommandForEcosystems(t *testing.T) {
	tests := []struct {
		name       string
		ecosystems []repo.Ecosystem
		want       []string
		wantErr    bool
	}{
		{name: "Go", ecosystems: []repo.Ecosystem{repo.EcosystemGo}, want: []string{"go", "test", "./..."}},
		{name: "Node", ecosystems: []repo.Ecosystem{repo.EcosystemNode}, want: []string{"npm", "test"}},
		{name: "Python", ecosystems: []repo.Ecosystem{repo.EcosystemPython}, want: []string{"python", "-m", "pytest"}},
		{name: "ambiguous", ecosystems: []repo.Ecosystem{repo.EcosystemGo, repo.EcosystemNode}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := &repo.Workspace{Ecosystems: test.ecosystems}
			switch test.name {
			case "Go":
				workspace.Files = []string{"main_test.go"}
			case "Node":
				workspace.Files = []string{"package.json"}
				workspace.Read = func(string) ([]byte, error) { return []byte(`{"scripts":{"test":"node test.js"}}`), nil }
			case "Python":
				workspace.Files = []string{"pyproject.toml", "test_smoke.py"}
				workspace.Read = func(path string) ([]byte, error) {
					if path == "pyproject.toml" {
						return []byte("[tool.pytest]\n"), nil
					}
					return nil, os.ErrNotExist
				}
			}
			command, err := testCommand(workspace)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected ambiguous ecosystem error")
				}
				return
			}
			if err != nil || strings.Join(command, " ") != strings.Join(test.want, " ") {
				t.Fatalf("command = %v, err = %v; want %v", command, err, test.want)
			}
		})
	}
}

func TestRenderVerifyReport(t *testing.T) {
	var output bytes.Buffer
	report := Report{ImageTag: "rebuildable-verify-test", LogDir: "logs", Stages: []StageResult{{Name: "build", Passed: true}, {Name: "test", Passed: false, Error: "test failed"}}}
	if err := RenderText(&output, report); err != nil {
		t.Fatalf("render report: %v", err)
	}
	if !strings.Contains(output.String(), "test failed") || !strings.Contains(output.String(), "rebuildable-verify-test") {
		t.Fatalf("report = %s", output.String())
	}
}

func TestPrepareLogDir(t *testing.T) {
	root := t.TempDir()
	path, err := prepareLogDir(root, "logs/run")
	if err != nil {
		t.Fatalf("prepare log directory: %v", err)
	}
	if !strings.HasPrefix(path, root) {
		t.Fatalf("log path %q escaped root %q", path, root)
	}
	if _, err := prepareLogDir(root, "/tmp/outside"); err == nil {
		t.Fatal("absolute log directory unexpectedly accepted")
	}
}

func TestRunRequiresExplicitTrust(t *testing.T) {
	_, err := Run(context.Background(), &repo.Workspace{Root: t.TempDir()}, Options{Timeout: time.Minute, Port: 8080})
	if !errors.Is(err, ErrTrustRequired) {
		t.Fatalf("error = %v, want ErrTrustRequired", err)
	}
}

func TestRunReportsInterruptionAndCleansUp(t *testing.T) {
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> \"$DOCKER_CALLS\"\nif [ \"$1\" = build ]; then exec sleep 30; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CALLS", calls)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	workspace := &repo.Workspace{Root: t.TempDir(), Files: []string{"Dockerfile"}}
	report, err := Run(ctx, workspace, Options{Timeout: time.Minute, Port: 8080, Trusted: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled (report = %+v)", err, report)
	}
	recorded, readErr := os.ReadFile(calls)
	if readErr != nil {
		t.Fatalf("read fake docker calls: %v", readErr)
	}
	if !strings.Contains(string(recorded), "rm --force") || !strings.Contains(string(recorded), "rmi --force") {
		t.Fatalf("cleanup did not run after interruption:\n%s", recorded)
	}
}
