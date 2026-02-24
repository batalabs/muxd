# Contributing

## Prerequisites

- **Go 1.25+**
- **git**

## Build & Run

```bash
go build -o muxd.exe .        # build
go run .                       # run (new session)
go run . -c                    # resume latest session
go vet ./...                   # lint
```

## Testing

```bash
go test ./...                  # run all tests
go test -v ./...               # verbose
go test -race ./...            # race detector
go test -cover ./...           # coverage
go test -run TestFoo ./...     # run specific test
```

### Conventions

- Every exported function and non-trivial unexported function must have tests.
- Use **table-driven tests**. Name subtests descriptively: `t.Run("returns error on empty input", ...)`.
- Use `testing.T` helpers: `t.Helper()`, `t.Cleanup()`, `t.TempDir()`.
- Never use `log.Fatal` or `os.Exit` in tests.
- Test files live next to the code: `store.go` → `store_test.go`.
- Use `testdata/` directories for fixtures.
- Mock external dependencies with interfaces, never hit real APIs in unit tests.
- For SQLite tests, use an in-memory database: `sql.Open("sqlite", ":memory:")`.
- Test function naming: `TestTypeName_MethodName` or `TestFunctionName`.

## Code Style

### Package structure

Code is organized into `internal/` sub-packages by domain. `main.go` is pure wiring. Do not add packages by type (`utils/`, `helpers/`, `common/`). Organize by domain.

### Naming

- **Files**: lowercase, single-word or hyphenated. One primary concern per file.
- **Types**: PascalCase nouns (`Session`, `Store`).
- **Functions**: verb-first for actions (`createSession`), noun for getters (`sessionTitle`).
- **Variables**: camelCase, short but descriptive.
- **Acronyms**: consistent casing (`apiKey`, `modelID`, not `modelId`).

### Error handling

- Always check errors. Wrap with `fmt.Errorf("doing thing: %w", err)`.
- Return errors up, don't panic (except for unrecoverable programmer errors).
- Use `errors.Is()` for matching, never string comparison.

### Functions

- Keep functions under ~50 lines.
- Accept interfaces, return concrete types.
- Bubble Tea `model` uses value receivers (returns modified copy).

### Concurrency

- Use Bubble Tea's `Cmd` pattern for async work.
- When goroutines are needed, ensure they can be cancelled via `context.Context` or channels.
- SQLite: single writer, WAL mode, wrap multi-step mutations in transactions.

### Formatting

- Code must pass `gofmt`.
- Run `go vet ./...` before committing.
- Imports: stdlib first, then third-party, separated by a blank line.
- No unused variables or imports.

## What NOT to Do

- Do not add `utils/`, `helpers/`, or `common/` packages.
- Do not add interfaces until you have 2+ concrete implementations.
- Do not use `init()` functions.
- Do not use global mutable state (beyond the `prog` variable).
- Do not add logging frameworks, use `fmt.Fprintf(os.Stderr, ...)`.
- Do not add CLI frameworks (cobra, urfave/cli).
- Do not use CGo-dependent SQLite drivers.
- Do not commit `.exe` binaries or IDE config files.

## Pull Requests

1. Create a branch from `main`.
2. Make your changes, ensuring tests pass (`go test ./...`) and lint is clean (`go vet ./...`).
3. Write or update tests for any new or changed behavior.
4. Keep commits focused, one logical change per commit.
5. Open a PR with a clear description of what changed and why.
