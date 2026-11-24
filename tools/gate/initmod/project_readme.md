# {{name}}

Say here what {{name}} is for.

## Development

Needs Go (version in `go.mod`), git and golangci-lint (version in
`.golangci-lint-version`).

```sh
go tool gate setup   # once: enables the git hooks
go tool gate         # every check; the hooks and CI run the same
go tool gate lint    # one step
```

Rules and layout: [docs/architecture.md](docs/architecture.md). Instructions
for coding agents: [AGENTS.md](AGENTS.md).

`internal/tasks` with `cmd/tasks-api` is the worked example from the template.
Follow its shape for the first bounded context, then delete both.

```sh
go run ./cmd/tasks-api   # TASKS_ADDR, default 127.0.0.1:8080
```

## Gate

The gate is a pinned tool dependency from [the template](https://{{upstream}}),
which documents its steps. Dependabot proposes newer versions; to update by hand:

```sh
go get -tool {{gate}}@latest
```
