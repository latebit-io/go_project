# AGENTS.md

Instructions for any coding agent working in this repository. Read
[docs/architecture.md](docs/architecture.md) before changing code: it states
the layout, import, naming and cleanup rules the gate enforces.

## Workflow

- When a task is complete run `go run ./tools/gate agent` and do what it says.
  Exit 0 means done. Output starting `QUESTION:` is for the user: relay it in
  your reply and stop. Any other failure: fix it and rerun.
- That step runs the whole gate, `go run ./tools/gate`; run the gate directly
  for full output while you work, or one step with `go run ./tools/gate lint`.
  Claude Code and pi run the agent step for you through their hooks.
- Simplify pass, when the user says yes to it: review the changed code for
  reuse, simplification, efficiency and depth, then apply the fixes. It is not
  a bug hunt. In Claude Code this is `/simplify`.
- Staging, when the user says yes to it: run `git add -A` and offer a commit
  subject. Never run `git commit`; the user commits.
- Branches: `<type>/<slug>` with a conventional type; never work on `main`.
- Commit subjects: `<type>(<scope>): <description>`, description five words
  at most, lowercase, no period. The reason goes in the body.
- `feat`, `fix`, `perf` and `test` branches that change Go code must add or
  change a test. Write the failing test first.
- No build constraints, platform file suffixes or cgo: every Go file builds on
  linux/amd64, darwin/arm64 and windows/amd64, and the gate cross-builds all three.
- `golangci-lint fmt` fixes formatting. Everything else is fixed in the code.
- Never weaken a guard rail to pass: no edits to the gate or what it reads
  (`tools/`, `.golangci.yml`, the hooks), and no `//nolint`, unless the user
  asked for that change.
- New bounded context: create `internal/<context>/{domain,app,adapters}`, start
  in `domain` with a test, wire it in `cmd/<name>/main.go` last. Follow the
  shape of `internal/tasks`; do not import it. A new `cmd/<name>` needs a
  `TestSmoke` that starts the real process, like
  `cmd/tasks-api/smoke_internal_test.go`.
- Builds go to `bin/` (`go build -o bin/ ./cmd/<name>`); never a bare `go build`.
- Dependencies: standard library first. A new module needs a reason, and is
  only linkable from `adapters`, `platform` and `cmd`.
