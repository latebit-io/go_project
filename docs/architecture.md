# Architecture

Rules are enforced by `tools/arch` (layout, directories, imports),
`tools/clean` (comments, emoji) and `.golangci.yml` (code, files, package
names). This page states them once.

## Layout

```
cmd/<name>/                      composition root, package main, wiring only
internal/<context>/domain/...    entities, value objects, domain events, ports
internal/<context>/app/...       use cases; orchestrate the domain through ports
internal/<context>/adapters/...  HTTP, SQL, queues: implementations of ports
internal/platform/...            technical plumbing shared by adapters
tools/...                        development tooling, never linked by production code
```

A package anywhere else fails the gate, including the module root, `pkg/`
and `internal/<context>/` itself. Directories the go tool skips (`testdata`,
`vendor`, dot and underscore names) are not checked and may not be imported.

Reference slice: `internal/tasks` wired by `cmd/tasks-api`.

| What                         | Where                                  |
|------------------------------|----------------------------------------|
| Entity, invariants, errors   | `internal/tasks/domain/task.go`        |
| Port                         | `internal/tasks/domain/repository.go`  |
| Use cases, injected clock/ID | `internal/tasks/app/service.go`        |
| Port implementation          | `internal/tasks/adapters/memory`       |
| DTOs, error-to-status map    | `internal/tasks/adapters/rest`         |
| Wiring                       | `cmd/tasks-api/main.go`                |

## Imports

| Layer      | Own context           | Any context | Standard library         | Outside modules |
|------------|-----------------------|-------------|--------------------------|-----------------|
| `domain`   | domain                | none        | no I/O, encoding, logging | no              |
| `app`      | domain, app           | none        | no I/O                   | no              |
| `adapters` | domain, app, adapters | platform    | all                      | yes             |
| `platform` | none                  | platform    | all                      | yes             |
| `cmd`      | all                   | all but tools | all                    | yes             |
| `tools`    | none                  | tools       | all                      | no              |

- Dependencies point inward: adapters → app → domain.
- A bounded context never imports another. `cmd` wires them: the consumer
  declares a port, an adapter implements it, `main` connects the two.
- Denied standard library packages per layer: `tools/arch/rules.go`.
- Test files follow the rules of the package they test.
- No build constraints: every Go file must build on every supported
  platform (linux/amd64, darwin/arm64, windows/amd64), so lint, tests and
  coverage see all the code everywhere. `//go:build` lines and `_linux.go`-style
  suffixes fail the gate, and the gate cross-builds each platform. The list is
  `Platforms` in `tools/arch/rules.go`.

## Naming

- Directories: lowercase letters and digits; `cmd/<name>` may use hyphens.
- Files: `snake_case.go`.
- Package name equals its directory, tagged files included; no `util`, `common`, `helpers`, `types`,
  `models`, `shared`, and no clash with a common standard library package.
- `package main` only under `cmd/<name>` and `tools/`.

## Code

- Errors: sentinel or typed, wrapped with `%w` when crossing a package; never discarded.
- No package-level state, no `init`, no `fmt.Print*`; inject `io.Writer`, clock, logger.
- Limits: function 50 lines, cyclomatic 10, cognitive 15, nesting 3, 4 arguments,
  3 results, interface 5 methods, file 300 lines, line 120 columns.
- Tests: black-box (`<pkg>_test`; `package main` uses `_internal_test.go`),
  `t.Parallel()`, shuffled, race detector wherever cgo is available and always
  in CI. Coverage: 80% of statements outside `cmd/`, counted over the module.
- Commands: each `cmd/<name>` has a `TestSmoke`; the gate requires the
  function and builds the command. The reference one starts the real process
  and drives it over HTTP.
- Tests come with the change: a branch that changes Go code must add or
  change a test unless its type is refactor, chore, docs, style, ci, build or revert.
- Comments: why, not what; limits under Cleanup. Longer reasoning goes in `docs/`.

## Cleanup

The mechanical part of a cleanup pass, checked on every run. Each item fails
the gate; judgment stays with `/simplify` and review.

| Pattern                                             | Enforced by                         |
|-----------------------------------------------------|-------------------------------------|
| `time.Sleep` anywhere, tests included               | forbidigo                           |
| `time.Now`, `Since`, `Until` outside cmd, platform  | forbidigo                           |
| `math/rand`, `crypto/rand`, `reflect` in domain     | `tools/arch`                   |
| `panic`, `recover`, `fmt.Print*`, `log.Print*`      | forbidigo                           |
| Narrating, change-log, placeholder, chat comments   | `tools/clean`                  |
| Filler adverbs, typographic punctuation in comments | `tools/clean`                  |
| Comment block over 3 lines, block comments included | `tools/clean`                  |
| "Do not remove", "keep in sync" pleas in comments   | `tools/clean`                  |
| Emoji anywhere in Go source                         | `tools/clean`                  |
| Commented-out code                                  | gocritic `commentedOutCode`         |
| Unused code, parameters, always-nil results         | unused, unparam, revive             |
| Deep nesting instead of early return                | nestif, revive                      |
| Discarded errors, unchecked type assertions         | errcheck, forcetypeassert           |

Time in tests: pass fixed times; for timers and timeouts use `testing/synctest`.
Directives (`//go:`, `//nolint:`) are exempt from the comment rules.

Not checkable by pattern, so left to review: needless defensive checks,
speculative abstractions, comments that are wrong.

## Changing a rule

A rule changes in its own commit, with the reason in the commit body.
Never loosen one to make unrelated work pass.

- Layer policy: `tools/arch/rules.go`, with a test in `check_test.go`.
- Branch and commit rules: `tools/gate/conv`. Coverage floor: `tools/gate/check`.
  Major-change threshold: `tools/gate/agent`.
- Comment patterns, cap, emoji ranges: `tools/clean/rules.go`, with a test in `check_test.go`.
- Lint: `.golangci.yml`; every disabled linter carries its reason on the same line.
- `//nolint` names the linter and explains: `//nolint:gosec // input is a constant`.
