# sbx - MicroVM Sandbox Management Tool

A CLI tool for creating and managing microVM sandboxes.

## Features

- Create sandboxes from YAML configuration
- Multiple sandbox engines:
  - **Docker**: Real containers for testing and development
  - **Fake**: Simulated engine for unit testing
  - Firecracker support planned for future releases
- SQLite-based persistent storage
- Comprehensive test coverage with integration tests

## Requirements

- Go 1.24+ (for building from source)
- Docker (for Docker engine support)

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

### Commands Overview

- `sbx create` - Create a new sandbox from configuration
- `sbx list` - List all sandboxes with filtering options
- `sbx status` - Show detailed information about a sandbox
- `sbx start` - Start a stopped sandbox
- `sbx stop` - Stop a running sandbox
- `sbx rm` - Remove a sandbox
- `sbx exec` - Execute a command in a running sandbox
- `sbx shell` - Open an interactive shell in a running sandbox

### Create a Sandbox

Create a new sandbox from a YAML configuration file:

```bash
sbx create -f sandbox.yaml
```

Example configuration (`sandbox.yaml`):

```yaml
name: example-sandbox
engine:
  docker:
    image: ubuntu:22.04
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

**Engine Configuration:**

Currently supported engines:

- **Docker Engine**: Runs sandboxes as Docker containers
  ```yaml
  engine:
    docker:
      image: ubuntu:22.04  # Any Docker image
  ```

- **Firecracker Engine** (planned for future release):
  ```yaml
  engine:
    firecracker:
      kernel_image: /path/to/vmlinux
      root_fs: /path/to/rootfs.ext4
  ```

Only one engine can be specified per sandbox configuration.

Override the sandbox name:

```bash
sbx create -f sandbox.yaml --name my-custom-name
```

### List Sandboxes

List all sandboxes in table format (default):

```bash
sbx list
```

Example output:
```
ID                           NAME               STATUS     CREATED
01JQYXZ2ABCDEFGH1234567890  example-sandbox    running    2 hours ago
01JQYZ3BCDEFGHIJ2345678901  test-sandbox       stopped    1 day ago
```

Filter by status:

```bash
sbx list --status running   # Show only running sandboxes
sbx list --status stopped   # Show only stopped sandboxes
```

Output in JSON format:

```bash
sbx list --format json
```

### Show Sandbox Status

Get detailed information about a specific sandbox by name or ID:

```bash
sbx status example-sandbox
```

Example output:
```
Name:       example-sandbox
ID:         01JQYXZ2ABCDEFGH1234567890
Status:     running
Engine:     docker
Image:      ubuntu:22.04
VCPUs:      2
Memory:     2048 MB
Disk:       10 GB
Created:    2026-01-30 10:30:45 UTC
Started:    2026-01-30 10:30:47 UTC
```

You can also use the sandbox ID:

```bash
sbx status 01JQYXZ2ABCDEFGH1234567890
```

Output in JSON format:

```bash
sbx status example-sandbox --format json
```

### Start a Sandbox

Start a stopped sandbox:

```bash
sbx start example-sandbox
```

The sandbox must be in `stopped` status to be started.

### Stop a Sandbox

Stop a running sandbox:

```bash
sbx stop example-sandbox
```

The sandbox must be in `running` status to be stopped.

### Remove a Sandbox

Remove a sandbox:

```bash
sbx rm example-sandbox
```

Force remove a running sandbox (stops it first):

```bash
sbx rm example-sandbox --force
```

### Execute Commands

Execute commands inside a running sandbox:

```bash
# Simple command execution
sbx exec example-sandbox -- ls -la

# Build a project inside sandbox
sbx exec example-sandbox -- go build ./...

# Run with working directory
sbx exec example-sandbox --workdir /app -- npm install

# With environment variables
sbx exec example-sandbox --env FOO=bar -- echo $FOO

# Multiple environment variables
sbx exec example-sandbox --env FOO=hello --env BAR=world -- env
```

**Exit Codes:**

The `exec` command propagates the exit code of the executed command. This makes it suitable for use in scripts:

```bash
# This will exit with code 1 if the command fails
sbx exec example-sandbox -- go test ./...
if [ $? -ne 0 ]; then
    echo "Tests failed!"
fi
```

**Stream Handling:**

Both stdout and stderr from the executed command are streamed in real-time to your terminal. You can also pipe input to the command:

```bash
# Pipe stdin to command
echo "Hello World" | sbx exec example-sandbox -- cat
```

### Interactive Shell

Open an interactive shell in a running sandbox:

```bash
# Open shell (uses /bin/sh)
sbx shell example-sandbox
```

The shell command is a convenience wrapper around `exec` that:
- Automatically runs `/bin/sh`
- Allocates a pseudo-TTY for interactive use
- Connects your terminal's stdin/stdout/stderr

**Note:** The sandbox must be in `running` status to execute commands or open a shell. If the sandbox is stopped, start it first with `sbx start`.

### Complete Lifecycle Example

Here's a complete workflow showing sandbox lifecycle with exec:

```bash
# Create a new sandbox
sbx create -f sandbox.yaml
# Output: Created sandbox: example-sandbox (01JQYXZ2ABCDEFGH1234567890)

# Execute a command immediately
sbx exec example-sandbox -- echo "Hello from sandbox"
# Output: Hello from sandbox

# Install tools in the sandbox
sbx exec example-sandbox -- apt-get update
sbx exec example-sandbox -- apt-get install -y python3

# Run a build command
sbx exec example-sandbox --workdir /app -- make build

# Open interactive shell for manual work
sbx shell example-sandbox

# Stop and cleanup when done
sbx stop example-sandbox
sbx rm example-sandbox
```

### Previous Workflow Example

Original sandbox lifecycle without exec:

```bash
# Create a new sandbox
sbx create -f sandbox.yaml
# Output: Created sandbox: example-sandbox (01JQYXZ2ABCDEFGH1234567890)

# List all sandboxes
sbx list
# Shows: example-sandbox with status "running"

# Check detailed status
sbx status example-sandbox

# Stop the sandbox
sbx stop example-sandbox
# Output: Stopped sandbox: example-sandbox

# Start it again
sbx start example-sandbox
# Output: Started sandbox: example-sandbox

# Remove the sandbox (must stop first or use --force)
sbx stop example-sandbox
sbx rm example-sandbox
# Output: Removed sandbox: example-sandbox

# Or force remove while running
sbx rm example-sandbox --force
```

### Global Options

All commands support:

```bash
--db-path /path/to/custom.db  # Use custom database path
```

Or via environment variable:

```bash
export SBX_DB_PATH=/path/to/custom.db
sbx list
```

## Development

### Prerequisites

- Go 1.24+
- Docker (for integration tests)
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
- **App Services**: 100% (create, list, status, start, stop, remove)
- **Printer**: 100% (table, JSON, time utilities)
- **Engine/Fake**: 96.4%
- **Storage/Memory**: 96.1%
- **Storage/SQLite**: 76.3%

### Generating Mocks

```bash
make go-gen
```

Mocks are generated for:
- `internal/sandbox.Engine`
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
│   └── commands/         # CLI commands (create, list, status, etc.)
├── internal/
│   ├── model/            # Domain models
│   ├── log/              # Logging interface
│   ├── printer/          # Output formatting (table, JSON)
│   ├── sandbox/           # Sandbox engine interface + implementations
│   ├── storage/          # Storage interface + implementations
│   └── app/              # Business logic services
│       ├── create/       # Create sandbox service
│       ├── list/         # List sandboxes service
│       ├── status/       # Get sandbox status service
│       ├── start/        # Start sandbox service
│       ├── stop/         # Stop sandbox service
│       └── remove/       # Remove sandbox service
├── test/integration/     # End-to-end tests
├── testdata/             # Example configs
└── scripts/check/        # CI scripts
```

## Architecture

The project follows a clean architecture pattern:

1. **Domain Layer** (`internal/model`): Core business models and validation
2. **Sandbox Layer** (`internal/sandbox`): Sandbox lifecycle management interface
3. **Storage Layer** (`internal/storage`): Persistence interface with SQLite implementation
4. **App Layer** (`internal/app`): Business logic orchestration for all operations
5. **Printer Layer** (`internal/printer`): Output formatting (table, JSON) with time utilities
6. **CLI Layer** (`cmd/sbx`): User-facing commands

### Dependencies

- `github.com/alecthomas/kingpin/v2` - CLI framework
- `github.com/oklog/ulid/v2` - ULID generation
- `modernc.org/sqlite` - Pure Go SQLite (no CGO)
- `github.com/golang-migrate/migrate/v4` - Database migrations
- `github.com/docker/docker` - Docker SDK for engine support
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/stretchr/testify` - Testing utilities

## License

MIT
