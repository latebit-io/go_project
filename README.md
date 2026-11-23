# go_project

Go coding constraints and guard rails: DDD layering, strict lint, one gate
shared by the git hooks, the agent task-complete step and CI. Works with any
agent, on Linux, macOS and Windows.

## Use as a template

```sh
go run ./tools/gate init github.com/acme/shop   # renames go.mod and every import
```

Then add the first bounded context under `internal/<context>/`, using the
example as the pattern.

## Example context

`internal/tasks` with `cmd/tasks-api` is a worked slice that passes every rule:
entity with an invariant, repository port, use cases, in-memory and HTTP
adapters, composition root, tests, smoke test.

```sh
go run ./cmd/tasks-api   # TASKS_ADDR, default 127.0.0.1:8080
```

Once your own context exists, delete `internal/tasks` and `cmd/tasks-api`.

## Setup

Needs Go (version in `go.mod`), git and golangci-lint (version in
`.golangci-lint-version`). The gate is a Go program, so there is no shell to
install on Windows. A C compiler is optional: without one, tests run without
the race detector locally; CI always runs it.

```sh
go run ./tools/gate setup   # enables .githooks: pre-commit and commit-msg
go run ./tools/gate         # runs the whole gate
go run ./tools/gate lint    # runs one step
```

The `Makefile` wraps the same commands for those who have `make`.

## Gate

| Step            | Checks                                               |
|-----------------|------------------------------------------------------|
| `tools`         | golangci-lint matches the pin; git hooks are enabled |
| `branch`        | `<type>/<slug>`; no work on `main`                   |
| `commits`       | conventional, description of five words at most      |
| `tidy`          | `go mod tidy -diff`                                  |
| `binaries`      | no compiled executable is staged                     |
| `tests-changed` | Go changes come with a test change                   |
| `arch`          | `tools/arch`: layout, imports, naming, build         |
| `clean`         | `tools/clean`: comment patterns, cap, emoji          |
| `format`        | gofumpt, gci (`golangci-lint fmt` to fix)            |
| `lint`          | `.golangci.yml`: all linters, forbidigo bans         |
| `cross`         | `go vet` for Linux, macOS and Windows                |
| `test`          | shuffled, race detector, 80% coverage floor          |
| `smoke`         | every `cmd/<name>` has a `TestSmoke` and builds      |

Vulnerabilities: `go run ./tools/gate vuln`, in CI and on demand (needs network).

Rules and layout: [docs/architecture.md](docs/architecture.md).

## Conventions

- Branch: `<type>/<slug>`, for example `feat/order-export`. Work on `main` is refused.
- Commit subject: `<type>(<scope>): <description>`, description five words at
  most; the body is free. Pull request titles follow the same rule.
- Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.
- A branch that changes Go code must add or change a test, unless its type is
  refactor, chore, docs, style, ci, build or revert.
- Merging to `main` needs a pull request with one approval, from CodeRabbit or
  a person, and every review thread resolved. A new push dismisses the approval.

## Agents

Works with any coding agent. Nothing in the gate depends on one.

| Part | How each agent gets it |
|---|---|
| Instructions | `AGENTS.md`, read natively by most agents; `CLAUDE.md` and `GEMINI.md` import it |
| Enforcement | CI runs the gate on every push; git hooks do locally once `setup` has run, and the gate fails until it has |
| Task-complete step | the agent runs `go run ./tools/gate agent`, as `AGENTS.md` tells it |

| Agent | Instructions | Task-complete step |
|---|---|---|
| Claude Code | `CLAUDE.md` imports `AGENTS.md` | automatic: hooks in `.claude/settings.json` |
| pi | `AGENTS.md` | automatic: `.pi/extensions/agent-gate.ts`, once you trust the project |
| opencode | `AGENTS.md` | the agent runs the step; no plugin shipped |
| Codex, Cursor, Copilot | `AGENTS.md` | the agent runs the step |
| Gemini CLI | `GEMINI.md` imports `AGENTS.md` | the agent runs the step |

To automate another agent, point its stop hook at
`go run ./tools/gate agent stop` and its prompt hook at
`go run ./tools/gate agent prompt`.

The step does three things:

1. After a major Go change (threshold in `tools/gate/agent`) it has the agent
   ask whether to run a simplify pass. Your next message is the answer.
2. It runs the gate. An unchanged tree reuses the last result.
3. When the gate passes it has the agent ask whether to stage the changes and
   offer a commit subject. You commit.

Exit 0 means done; otherwise the output is either the gate's failure or a line
starting `QUESTION:` for you. Hooks run the step on every stop; without them
it rests on the agent following `AGENTS.md`. Either way the git hooks and CI
are the backstop.

## Release

Push a `vX.Y.Z` tag: CI runs the gate, then publishes a GitHub release.
