# sbx - MicroVM Sandbox Management Tool

A CLI tool for creating and managing microVM sandboxes.

## Features

- Create sandboxes from YAML configuration
- SQLite-based persistent storage
- Fake engine for testing (real engine TBD)
- Comprehensive test coverage

## Installation

```bash
go install github.com/slok/sbx/cmd/sbx@latest
```

Or build from source:

```bash
make build
# Binary will be in bin/sbx
```

## Usage

### Create a Sandbox

```bash
sbx create -f sandbox.yaml
```

Example configuration (`sandbox.yaml`):

```yaml
name: example-sandbox
base: ubuntu:22.04
packages:
  - curl
  - git
  - vim
env:
  ENVIRONMENT: development
  LOG_LEVEL: debug
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
```

### Override Sandbox Name

```bash
sbx create -f sandbox.yaml --name my-custom-name
```

### Use Custom Database Path

```bash
sbx create -f sandbox.yaml --db-path /path/to/custom.db
```

Or via environment variable:

```bash
export SBX_DB_PATH=/path/to/custom.db
sbx create -f sandbox.yaml
```

## Development

### Prerequisites

- Go 1.24+
- make
- mockery (for generating mocks)

### Running Tests

```bash
# Unit tests only
make test

# Integration tests only
make test-integration

# All tests
make test-all

# With coverage
make ci-test
```

### CI/CD

The project uses GitHub Actions for continuous integration:

- **Check Job**: Runs `golangci-lint` for code quality checks
- **Unit Test Job**: Runs all unit tests with race detector and coverage reporting
- **Integration Test Job**: Runs end-to-end CLI tests
- **Build Job**: Builds the binary and uploads as artifact
- **Tagged Release Job**: Creates GitHub releases with binaries on version tags

#### CI Scripts

- `scripts/check/check.sh` - Runs golangci-lint
- `scripts/check/unit-test.sh` - Runs unit tests with race detector and coverage
- `scripts/check/integration-test.sh` - Runs integration tests

#### Test Coverage

Current coverage by layer:
- **Model**: 100%
- **App/Create**: 100%
- **Engine/Fake**: 96.4%
- **Storage/Memory**: 96.1%
- **Storage/SQLite**: 76.3%

### Generating Mocks

```bash
make go-gen
```

Mocks are generated for:
- `internal/engine.Engine`
- `internal/storage.Repository`

### Code Quality

```bash
# Run linters
make check

# CI-style checks (requires golangci-lint)
make ci-check
```

## Project Structure

```
sbx/
├── cmd/sbx/              # CLI entry point
│   ├── main.go
│   └── commands/         # CLI commands
├── internal/
│   ├── model/            # Domain models
│   ├── log/              # Logging interface
│   ├── engine/           # Engine interface + implementations
│   ├── storage/          # Storage interface + implementations
│   └── app/create/       # Create service business logic
├── test/integration/     # End-to-end tests
├── testdata/             # Example configs
└── scripts/check/        # CI scripts
```

## Architecture

The project follows a clean architecture pattern:

1. **Domain Layer** (`internal/model`): Core business models and validation
2. **Engine Layer** (`internal/engine`): Sandbox lifecycle management interface
3. **Storage Layer** (`internal/storage`): Persistence interface with SQLite implementation
4. **App Layer** (`internal/app`): Business logic orchestration
5. **CLI Layer** (`cmd/sbx`): User-facing commands

### Dependencies

- `github.com/alecthomas/kingpin/v2` - CLI framework
- `github.com/oklog/ulid/v2` - ULID generation
- `modernc.org/sqlite` - Pure Go SQLite (no CGO)
- `github.com/golang-migrate/migrate/v4` - Database migrations
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/stretchr/testify` - Testing utilities

## License

MIT
