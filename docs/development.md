# Development

## Prerequisites

- Go 1.24+
- make
- [mockery](https://vektra.github.io/mockery/) (for generating mocks)
- Linux with KVM (for running Firecracker sandboxes)

## Building

```bash
make build
# Binary: ./bin/sbx (with CAP_NET_ADMIN set via setcap)
```

For development iteration, prefer `go run` over building:

```bash
go run ./cmd/sbx create --name test --engine fake --cpu 2 --mem 2048 --disk 10
go run ./cmd/sbx list
go run ./cmd/sbx start test
```

The `fake` engine is useful for testing CLI workflows without real VMs.

## Testing

### Unit Tests

```bash
make test
```

Runs `go test -v -race ./internal/... ./pkg/...`. Fast feedback loop — should complete in seconds.

Unit tests use:
- **Fake engine** — In-memory, no-op sandbox operations
- **Memory storage** — In-memory repository for isolation
- **Table-driven tests** — `map[string]struct{...}` pattern with descriptive keys
- **Mockery mocks** — Generated from interfaces (see [Generating Mocks](#generating-mocks))

### Integration Tests

```bash
make test-integration
```

Integration tests live in `test/integration/` and test end-to-end CLI flows. These are designed for CI — they may require real infrastructure (SQLite files, network access).

### All Tests

```bash
make test-all          # unit + integration
make ci-test           # unit tests (CI mode, via scripts/check/unit-test.sh)
make ci-integration-test  # integration tests (CI mode)
```

## Code Quality

```bash
make check       # go vet + go fmt
make ci-check    # golangci-lint (CI mode, via scripts/check/check.sh)
```

## Generating Mocks

Mocks are generated with [mockery](https://vektra.github.io/mockery/) from the `.mockery.yml` config at the project root.

```bash
make go-gen
```

Mocked interfaces:
- `internal/sandbox.Engine`
- `internal/storage.Repository`
- `internal/image.ImageManager`

Mock packages follow the naming convention `{package}mock/mocks.go` (e.g., `internal/sandbox/sandboxmock/mocks.go`).

To add a new mock:
1. Add the interface to `.mockery.yml` under `packages:`
2. Run `make go-gen`
3. Import as `"github.com/slok/sbx/internal/{package}/{package}mock"`

## CI/CD

The project uses GitHub Actions:

| Job | Description |
|-----|-------------|
| **Check** | Runs `golangci-lint` |
| **Unit Test** | Runs unit tests with race detector and coverage |
| **Integration Test** | Runs end-to-end CLI tests |
| **Build** | Builds binary, uploads as artifact |
| **Release** | Creates GitHub releases with binaries on version tags |

CI scripts:
- `scripts/check/check.sh` — Linting
- `scripts/check/unit-test.sh` — Unit tests with coverage
- `scripts/check/integration-test.sh` — Integration tests

## Project Conventions

### Code Style

- Standard Go conventions (`gofmt`, `go vet`)
- Self-documenting code — clear names over comments
- `context.Context` as first argument on all public functions
- Config struct pattern for dependency injection with `defaults()` validation
- Sentinel errors wrapped with context: `fmt.Errorf("...: %w: %w", err, internalerrors.ErrNotFound)`

### Testing Style

- Table-driven: `map[string]struct{` with descriptive test names as keys
- Mock setup via functions: `mock func(mc *sandboxmock.Engine)` fields
- `assert` for soft checks, `require` for fatal checks

### Git Workflow

- Never commit directly to main
- Branch naming: `{username}/branch-name`
- Prefer small, single-commit PRs
- Commit: `git add . && git commit -svm "message"`
- Run unit tests before creating PRs

### Generated Code

Never edit generated files directly. Edit the source, then run `make go-gen`.
