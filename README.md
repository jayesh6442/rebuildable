# Rebuildable

Rebuildable tells you whether a stranger can clone and run a repository on a clean Linux machine—and generates the missing reproducibility files.

The CLI checks the same reproducibility rules a reviewer cares about: Git history, dependency lockfiles, pinned runtimes, containers, tests, documentation, environment variables, and machine-specific paths. Static scans use tracked project text when Git is available, or the present working tree for a local non-Git directory. They skip dependency trees, symlinks, binaries, and files over 1 MB.

## Setup

Rebuildable is written in Go. Install Go 1.27 or newer. Dependencies are vendored so the test suite can run without network access.

```sh
go version
go install ./cmd/rebuildable
```

Docker is only needed for the verification command. The audit, fix, and default test suite do not require API keys. Dependencies are vendored for offline builds.

## Clean Linux rebuild

From a fresh clone on Linux, these are the complete build, test, audit, and
container commands:

```sh
git clone https://github.com/jayesh6442/rebuildable.git
cd rebuildable
GOPROXY=off make build
GOPROXY=off make test
GOPROXY=off make lint
./bin/rebuildable audit . --min-score 8
docker build --tag rebuildable .
docker run --rm rebuildable
```

The first Docker build may download the pinned Go base image. After the image
is available locally, the build itself uses the vendored Go dependencies.

## Run

Audit the current repository:

```sh
go run ./cmd/rebuildable audit .
```

Audit a local path or shallow-clone a Git URL:

```sh
go run ./cmd/rebuildable audit ./path/to/repository
go run ./cmd/rebuildable audit https://example.com/owner/project.git
```

Write machine-readable reports and enforce a CI threshold:

```sh
go run ./cmd/rebuildable audit . --json
go run ./cmd/rebuildable audit . --md audit.md
go run ./cmd/rebuildable audit . --min-score 8
```

Generate missing reproducibility files. The default is a dry-run diff:

```sh
go run ./cmd/rebuildable fix .
go run ./cmd/rebuildable fix . --write
go run ./cmd/rebuildable fix . --write --force
go run ./cmd/rebuildable fix . --only dockerfile,env,readme,test,pins
```

Prove the repository in Docker:

```sh
go run ./cmd/rebuildable verify . --trust --timeout 10m --keep-logs
```

Verification executes repository-controlled Docker build instructions and application code, so it requires the explicit `--trust` flag. It builds with `docker build --no-cache`, builds a dedicated test stage when the Dockerfile provides one, starts the application image, probes an explicitly exposed port on localhost, runs the test command in an isolated container, applies CPU/memory/process limits, disables runtime networking unless a localhost HTTP probe needs it, and cleans up its image and containers. Logs are retained on failure and can be retained on success with `--keep-logs`.

The audit implements all eight static checks. Warnings and failures do not earn points; only full passes count toward the score. An `8/8` audit is static evidence; run `verify --trust` to prove Docker build, startup, and tests.

Build and run the self-auditing image:

```sh
docker build --tag rebuildable .
docker run --rm rebuildable
```

Typical audit output:

```text
Rebuildable · . · go

  ✓ Git repo with commits          12 commit(s) found
  ✓ Dependency lockfile            Go checksum file found
  ✓ Runtime version pinned         Go 1.27 with toolchain go1.27.2
  ✓ Dockerfile builds and runs     static FROM and run instructions found
  ✓ One test command               test target runs go test ./...
  ✓ README with setup, run, test   setup, run, and test sections found
  ✓ .env.example / secrets         no tracked .env, hard-coded secret, or undocumented environment variable found
  ✓ No absolute paths              no home-directory absolute paths found in tracked text files

  Score 8/8 · 0 fixable with `rebuildable fix --write`
```

## Test

Run the offline test suite:

```sh
make test
```

Run static analysis:

```sh
make lint
```

Run the reproducibility demo:

```sh
make demo
```

Docker-backed integration checks are kept separate:

```sh
make test-integration
```

## Development status

The intended end state is:

```text
rebuildable audit [path|git-url]
rebuildable fix [path] [--write]
rebuildable verify [path] --trust
```

The audit and fix flows remain usable without Docker, network access, or API keys. Verification requires a local Docker daemon and trusted repository code.

## Supported scope

The production MVP supports root-level Go, Node.js, and Python projects. It does not yet traverse monorepos or claim support for Rust, Java, Ruby, or .NET. Node lockfiles for npm, pnpm, Yarn, and Bun are recognized; Python supports `uv.lock`, `poetry.lock`, and fully pinned `requirements.txt`.

## Security model

`audit` reads repository files without executing project code. `fix` writes only generated files beneath the requested local workspace. `verify` is different: Dockerfiles, build steps, application startup, and test commands are untrusted code execution. Rebuildable requires `--trust`, uses disposable named resources, localhost-only port publication, runtime limits, and cleanup. Use a rootless/disposable Docker environment for repositories you do not control.

## GitHub Action

The included workflow fetches full Git history, runs the audit, offline tests, lint, Docker integration tests, and the self-auditing image smoke test. It uses the same local CLI and does not need a secret.

## Exit codes

- `0` — the audit completed and met `--min-score` (or no threshold was supplied).
- `1` — the audit completed but scored below `--min-score`.
- `2` — the CLI could not load the repository, clone the Git URL, or write a report.
