package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jayeshsarvaiya/rebuildable/internal/checks"
	"github.com/jayeshsarvaiya/rebuildable/internal/fix"
	"github.com/jayeshsarvaiya/rebuildable/internal/repo"
	"github.com/jayeshsarvaiya/rebuildable/internal/report"
	"github.com/jayeshsarvaiya/rebuildable/internal/verify"
	"github.com/jayeshsarvaiya/rebuildable/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newRootCommand().ExecuteContext(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func exitCode(err error) int {
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	return 2
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "rebuildable",
		Short:         "Audit a repository for reproducibility",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newAuditCommand())
	root.AddCommand(newFixCommand())
	root.AddCommand(newVerifyCommand())
	return root
}

func newAuditCommand() *cobra.Command {
	var asJSON bool
	var markdownPath string
	var minimumScore int
	command := &cobra.Command{
		Use:   "audit [path]",
		Short: "Audit a local repository or Git URL",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			started := time.Now()
			if minimumScore < -1 || minimumScore > 8 || (minimumScore == -1 && cmd.Flags().Changed("min-score")) {
				return fmt.Errorf("--min-score must be between 0 and 8")
			}
			path := "."
			if len(args) == 1 {
				path = args[0]
			}

			loaded, cleanup, err := repo.Load(cmd.Context(), path)
			if err != nil {
				return err
			}
			defer cleanup()

			results := checks.RunAll(cmd.Context(), loaded)
			var minimum *int
			if minimumScore >= 0 {
				minimum = &minimumScore
			}

			if markdownPath != "" {
				var markdownOutput strings.Builder
				if err := report.RenderMarkdown(&markdownOutput, loaded, results, minimum); err != nil {
					return fmt.Errorf("render Markdown report: %w", err)
				}
				if err := os.WriteFile(markdownPath, []byte(markdownOutput.String()), 0o644); err != nil {
					return fmt.Errorf("write Markdown report: %w", err)
				}
			}

			var renderErr error
			if asJSON {
				renderErr = report.RenderJSON(cmd.OutOrStdout(), loaded, results, minimum)
			} else {
				renderErr = report.RenderTextWithOptions(cmd.OutOrStdout(), loaded, results, terminalColor(cmd.OutOrStdout()), minimum, time.Since(started))
			}
			if renderErr != nil {
				return renderErr
			}
			if minimum != nil && checks.Score(results) < *minimum {
				return &exitError{code: 1, err: fmt.Errorf("score %d is below minimum %d", checks.Score(results), *minimum)}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "write a JSON report to stdout")
	command.Flags().StringVar(&markdownPath, "md", "", "write a Markdown report to this file")
	command.Flags().IntVar(&minimumScore, "min-score", -1, "fail with exit code 1 below this score (0-8)")
	return command
}

func terminalColor(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func newFixCommand() *cobra.Command {
	var write bool
	var force bool
	var only []string
	var ecosystem string
	command := &cobra.Command{
		Use:   "fix [path]",
		Short: "Generate missing reproducibility files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			workspace, cleanup, err := repo.LoadWorkspace(cmd.Context(), path)
			if err != nil {
				return err
			}
			defer cleanup()
			plan, err := fix.PlanChanges(cmd.Context(), workspace, fix.Options{Only: only, Force: force, Ecosystem: ecosystem})
			if err != nil {
				return err
			}
			if !write {
				return fix.RenderUnifiedDiff(cmd.OutOrStdout(), plan)
			}
			applied, err := fix.ApplyChanges(cmd.Context(), workspace, plan, force)
			if err != nil {
				return err
			}
			for _, change := range applied.Applied {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", change.Path); err != nil {
					return err
				}
			}
			for _, change := range applied.Skipped {
				if change.Action == fix.ActionNoop {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "unchanged %s\n", change.Path); err != nil {
						return err
					}
				}
			}
			if len(applied.Conflicts) > 0 {
				for _, change := range applied.Conflicts {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "protected %s (use --force to replace)\n", change.Path); err != nil {
						return err
					}
				}
				return &exitError{code: 1, err: fmt.Errorf("%d existing file(s) require --force", len(applied.Conflicts))}
			}
			if len(applied.Applied) == 0 && len(applied.Skipped) == 0 {
				_, err := io.WriteString(cmd.OutOrStdout(), "No generated changes.\n")
				return err
			}
			return nil
		},
	}
	command.Flags().BoolVar(&write, "write", false, "apply generated files instead of showing a diff")
	command.Flags().BoolVar(&force, "force", false, "allow replacing existing files when used with --write")
	command.Flags().StringSliceVar(&only, "only", nil, "limit generators: dockerfile,env,pins,readme,test")
	command.Flags().StringVar(&ecosystem, "ecosystem", "", "select an ecosystem when multiple are detected")
	return command
}

func newVerifyCommand() *cobra.Command {
	var timeout time.Duration
	var port int
	var keepLogs bool
	var logDir string
	var trust bool
	command := &cobra.Command{
		Use:   "verify [path]",
		Short: "Build and test a repository in Docker",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			workspace, cleanup, err := repo.LoadWorkspace(cmd.Context(), path)
			if err != nil {
				return err
			}
			defer cleanup()
			verification, err := verify.Run(cmd.Context(), workspace, verify.Options{Timeout: timeout, Port: port, KeepLogs: keepLogs, LogDir: logDir, Trusted: trust})
			if err != nil {
				return err
			}
			if err := verify.RenderText(cmd.OutOrStdout(), verification); err != nil {
				return err
			}
			if !verification.Passed() {
				return &exitError{code: 1, err: fmt.Errorf("verification failed")}
			}
			return nil
		},
	}
	command.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "timeout for each Docker stage")
	command.Flags().IntVar(&port, "port", 8080, "port to probe when the container stays running")
	command.Flags().BoolVar(&keepLogs, "keep-logs", false, "keep verification logs")
	command.Flags().StringVar(&logDir, "log-dir", "", "workspace-relative directory for verification logs")
	command.Flags().BoolVar(&trust, "trust", false, "allow execution of repository-controlled Docker build and runtime commands")
	return command
}
