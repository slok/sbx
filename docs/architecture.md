# Architecture

## Project Structure

```
sbx/
├── cmd/sbx/                  # CLI entry point
│   ├── main.go               # App bootstrap, signal handling
│   └── commands/             # One file per CLI command
├── internal/
│   ├── model/                # Domain models, validation, sentinel errors
│   ├── app/                  # Business logic (one service per operation)
│   │   ├── create/           # Create sandbox
│   │   ├── start/            # Start sandbox (session config, env injection)
│   │   ├── stop/             # Stop sandbox
│   │   ├── remove/           # Remove sandbox
│   │   ├── list/             # List sandboxes
│   │   ├── status/           # Get sandbox status
│   │   ├── exec/             # Execute command in sandbox
│   │   ├── copy/             # Copy files to/from sandbox
│   │   ├── forward/          # Port forwarding
│   │   ├── imagecreate/      # Create snapshot image from sandbox
│   │   ├── imagelist/        # List images
│   │   ├── imagepull/        # Pull image release
│   │   ├── imagerm/          # Remove image
│   │   └── imageinspect/     # Inspect image manifest
│   ├── sandbox/              # Engine interface + implementations
│   │   ├── engine.go         # Engine interface definition
│   │   ├── fake/             # In-memory engine for testing
│   │   └── firecracker/      # Firecracker microVM engine
│   ├── storage/              # Persistence
│   │   ├── storage.go        # Repository interface
│   │   ├── sqlite/           # SQLite implementation (with migrations)
│   │   ├── memory/           # In-memory implementation (for testing)
│   │   └── io/               # YAML config/session loader
│   ├── image/                # Image manager (GitHub releases + local images)
│   ├── proxy/                # Egress proxy (HTTP, TLS/SNI, DNS)
│   ├── printer/              # Output formatters (table, JSON)
│   ├── log/                  # Logging interface
│   └── task/                 # Task tracking for multi-step operations
├── pkg/lib/                  # Public Go SDK
│   └── log/                  # Re-exported logger interface
├── examples/
│   ├── opencode/             # Real-world SDK example (AI coding sandbox)
│   └── sessions/             # Example session YAML files
├── docs/                     # Documentation
├── test/integration/         # End-to-end CLI tests
├── scripts/                  # CI and build scripts
└── testdata/                 # Test fixtures
```

## Layers

The project follows a clean architecture with strict dependency direction:

```
CLI (cmd/sbx/)  ───┐
                   ├──▶  App Services (internal/app/)  ──▶  Engine (internal/sandbox/)
SDK (pkg/lib/)  ───┘           │                                    │
                               │                              Firecracker / Fake
                               ▼
                        Storage (internal/storage/sqlite/)
```

### 1. Domain Layer (`internal/model/`)

Core types and business rules. No dependencies on other layers.

- `Sandbox`, `SandboxConfig`, `Resources` — core domain models
- `FirecrackerEngine` — engine-specific config
- `SessionConfig`, `EgressPolicy`, `EgressRule` — session/networking models
- `Image`, `ImageManifest` — image models
- Validation logic and sentinel errors (`ErrNotFound`, `ErrNotValid`, `ErrAlreadyExists`)

### 2. Engine Layer (`internal/sandbox/`)

Sandbox lifecycle management behind the `Engine` interface:

```go
type Engine interface {
    Create(ctx, sandbox) error
    Start(ctx, sandbox, StartOpts) error
    Stop(ctx, sandbox) error
    Remove(ctx, sandbox) error
    Status(ctx, sandbox) (Status, error)
    Exec(ctx, sandbox, command, ExecOpts) (ExecResult, error)
    CopyTo(ctx, sandbox, src, dst) error
    CopyFrom(ctx, sandbox, src, dst) error
    Forward(ctx, sandbox, ports) error
    Check(ctx) ([]CheckResult, error)
}
```

Implementations:
- **Firecracker** — Real microVMs with TAP networking, SSH access, nftables, egress proxy
- **Fake** — In-memory, no-op engine for unit testing and development

### 3. Storage Layer (`internal/storage/`)

Persistence behind the `Repository` interface. SQLite implementation with migration-based schema management. Pure Go SQLite (`modernc.org/sqlite`) — no CGO required.

### 4. App Layer (`internal/app/`)

Business logic orchestration. Each operation is a standalone service with:
- Config struct with dependency injection
- `defaults()` validation method
- Single `Run()` method

Services coordinate between engine, storage, and image management.

### 5. Printer Layer (`internal/printer/`)

Output formatting. Supports table (human) and JSON (machine) formats. Used by CLI commands that display data.

### 6. CLI Layer (`cmd/sbx/`)

User-facing commands using `kingpin/v2`. Each command file wires dependencies, calls the appropriate app service, and formats output.

### 7. SDK Layer (`pkg/lib/`)

Public Go SDK. Thin wrapper around the same app services the CLI uses. Maps between public types (`pkg/lib/model.go`) and internal types (`internal/model/`). Provides sentinel errors for programmatic error handling.

## Key Design Decisions

- **One service per operation** — Each app service does one thing. No god services.
- **Engine as interface** — Swap implementations without touching business logic. Fake engine enables fast testing.
- **Pure Go SQLite** — `modernc.org/sqlite` avoids CGO, simplifying builds and cross-compilation.
- **Session config is ephemeral** — Environment and egress policies are per-start, not persisted with the sandbox.
- **Images as filesystem** — Image metadata stored as `manifest.json` files, not in the database.
- **Task tracking** — Multi-step engine operations (create, start) use a task system for crash recovery.
- **Deterministic networking** — IP and MAC addresses derived from sandbox ID hash, avoiding DHCP.

## Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/alecthomas/kingpin/v2` | CLI framework |
| `modernc.org/sqlite` | Pure Go SQLite (no CGO) |
| `github.com/golang-migrate/migrate/v4` | Database migrations |
| `github.com/oklog/ulid/v2` | ULID generation for sandbox IDs |
| `github.com/google/nftables` | Firewall rules (nftables) |
| `github.com/pkg/sftp` | SFTP file transfer |
| `golang.org/x/crypto/ssh` | SSH for VM access |
| `github.com/sirupsen/logrus` | Structured logging |
| `github.com/stretchr/testify` | Testing utilities |
