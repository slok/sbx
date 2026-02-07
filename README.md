# sbx - MicroVM Sandbox Management Tool

A CLI tool for creating and managing microVM sandboxes.

## Features

- Create sandboxes from YAML configuration
- Multiple sandbox engines:
  - **Firecracker**: MicroVM sandboxes
  - **Fake**: Simulated engine for unit testing
- SQLite-based persistent storage
- Comprehensive test coverage with integration tests

## Requirements

- Go 1.24+ (for building from source)
- Firecracker host requirements (KVM, networking tools)

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
- `sbx snapshot create` - Create a rootfs snapshot from a stopped sandbox

### Create a Sandbox

Create a new sandbox:

```bash
sbx create --name example-sandbox --engine firecracker \
  --firecracker-root-fs /path/to/rootfs.ext4 \
  --firecracker-kernel /path/to/vmlinux
```

Create a sandbox directly from a snapshot (auto-uses snapshot rootfs):

```bash
sbx create --name example-sandbox-2 --engine firecracker \
  --from-snapshot base-dev-image \
  --firecracker-kernel /path/to/vmlinux
```

When `--from-snapshot` is used, `--firecracker-root-fs` must not be provided.

Example configuration (`sandbox.yaml`):

```yaml
name: example-sandbox
engine:
  firecracker:
    kernel_image: /path/to/vmlinux
    root_fs: /path/to/rootfs.ext4
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

- **Firecracker Engine**:
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
Engine:     firecracker
RootFS:     /path/to/rootfs.ext4
Kernel:     /path/to/vmlinux
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

### Create a Snapshot

Create a rootfs snapshot from an existing sandbox:

```bash
sbx snapshot create example-sandbox base-dev-image
```

You can omit the snapshot name, and `sbx` will auto-generate one as:

```text
<sandbox-name>-<YYYYMMDD-HHMM>
```

Example:

```bash
sbx snapshot create example-sandbox
# example-sandbox-20260207-0935
```

Important snapshot behavior in v1:

- Snapshot source sandbox must be in `created` or `stopped` status
- Snapshot names are globally unique and can only use `[a-zA-Z0-9._-]`
- Snapshots are stored under `~/.sbx/snapshots` and remain even if the source sandbox is removed
- Snapshot captures rootfs disk state only (no memory/device state)

Use snapshot as source for `create`:

```bash
sbx create --name restored-sandbox --engine firecracker \
  --from-snapshot base-dev-image \
  --firecracker-kernel /path/to/vmlinux
```

### Complete Lifecycle Example

Here's a complete workflow showing sandbox lifecycle:

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

#### Image Scripts

- `scripts/setup-firecracker-images.sh` - Downloads Firecracker kernel/rootfs test artifacts
- `scripts/images/alpine/build-rootfs.sh` - Builds Alpine rootfs images for SBX Firecracker sandboxes

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
└── scripts/              # CI and image scripts
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
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/stretchr/testify` - Testing utilities

## License

MIT
