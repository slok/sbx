# sbx

Lightweight, secure microVM sandboxes powered by [Firecracker](https://firecracker-microvm.github.io/). Create isolated environments in milliseconds, run commands, control network egress, and manage everything from the CLI or Go SDK.

## Features

- **Fast microVMs** — Firecracker sandboxes boot in ~125ms
- **Full lifecycle** — Create, start, stop, remove sandboxes
- **Exec & shell** — Run commands or open interactive shells inside sandboxes
- **File transfer** — Copy files between host and sandbox (SCP-based)
- **Port forwarding** — Forward local ports to sandbox services via SSH tunnels
- **Session config** — Inject environment variables and egress policies per start
- **Egress filtering** — HTTP/TLS/DNS proxy with domain allowlists (no MITM)
- **Image management** — Pull pre-built images or create snapshots from sandboxes
- **Go SDK** — Full programmatic access via `github.com/slok/sbx/pkg/lib`
- **Pure Go** — SQLite storage with no CGO dependencies

## Requirements

- Linux with KVM support (`/dev/kvm`)
- Root or `CAP_NET_ADMIN` capability (for TAP devices and nftables)
- Go 1.24+ (building from source only)

## Installation

```bash
go install github.com/slok/sbx/cmd/sbx@latest
```

Or build from source:

```bash
git clone https://github.com/slok/sbx.git && cd sbx
make build
# Binary: ./bin/sbx
```

## Quick Start

```bash
# Pull a pre-built image (kernel + rootfs + firecracker binary)
sbx image pull v0.1.0

# Create a sandbox from the image
sbx create --name my-sandbox --engine firecracker --from-image v0.1.0

# Start it
sbx start my-sandbox

# Run a command
sbx exec my-sandbox -- cat /etc/os-release

# Open an interactive shell
sbx shell my-sandbox

# Stop and clean up
sbx stop my-sandbox
sbx rm my-sandbox
```

## Commands

| Command | Description |
|---------|-------------|
| `sbx create` | Create a new sandbox |
| `sbx start` | Start a stopped sandbox (with optional session config) |
| `sbx stop` | Stop a running sandbox |
| `sbx rm` | Remove a sandbox (`--force` to stop first) |
| `sbx list` | List sandboxes (filter by `--status`, output `--format json`) |
| `sbx status` | Show detailed sandbox information |
| `sbx exec` | Execute a command inside a running sandbox |
| `sbx shell` | Open an interactive shell in a sandbox |
| `sbx cp` | Copy files between host and sandbox |
| `sbx forward` | Forward local ports to a sandbox |
| `sbx snapshot` | Create a snapshot image from a sandbox |
| `sbx image list` | List available images (releases + snapshots) |
| `sbx image pull` | Pull a pre-built image |
| `sbx image rm` | Remove a local image |
| `sbx image inspect` | Inspect an image manifest |
| `sbx doctor` | Run preflight health checks |

See [docs/commands.md](docs/commands.md) for the full reference with all flags and options.

## Common Patterns

### Dev Environment

```bash
# Create and start with environment variables
sbx create --name dev --engine firecracker --from-image v0.1.0 --disk 20
sbx start dev --env APP_ENV=development --env LOG_LEVEL=debug

# Install tools, copy project files, forward ports
sbx exec dev -- apk add git nodejs npm
sbx cp ./my-project dev:/workspace
sbx forward dev 3000 8080
```

### Agentic / AI Sandbox with Egress Control

```bash
# Create a sandbox for an AI agent with restricted network
sbx create --name agent-sandbox --engine firecracker --from-image v0.1.0

# Start with session config: env vars + egress allowlist
sbx start agent-sandbox -f session.yaml --env ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY

# Agent runs inside the sandbox with controlled access
sbx exec agent-sandbox -- /usr/local/bin/my-agent

# Snapshot the result for later inspection
sbx stop agent-sandbox
sbx snapshot agent-sandbox --name agent-result-001
```

### Snapshot & Restore

```bash
# Set up a base environment
sbx create --name base --engine firecracker --from-image v0.1.0
sbx start base
sbx exec base -- apk add python3 py3-pip git
sbx stop base

# Snapshot it as a reusable image
sbx snapshot base --name python-dev

# Create new sandboxes from the snapshot (instant clones)
sbx create --name project-a --engine firecracker --from-image python-dev
sbx create --name project-b --engine firecracker --from-image python-dev
```

## Session Configuration

Sessions are ephemeral, per-start configuration. Unlike sandbox config (set at creation), session config can change every time you start a sandbox. This is useful for injecting secrets, toggling environments, or controlling network access.

Pass a session file with `-f` or individual env vars with `--env`:

```bash
sbx start my-sandbox -f session.yaml --env EXTRA_VAR=value
```

Example `session.yaml`:

```yaml
name: dev
env:
  DATABASE_URL: postgres://localhost:5432/mydb
  API_KEY: sk-secret-key

egress:
  default: deny
  rules:
    - { domain: "github.com", action: allow }
    - { domain: "*.github.com", action: allow }
    - { domain: "registry.npmjs.org", action: allow }
    - { domain: "api.openai.com", action: allow }
```

The egress filter intercepts HTTP, TLS (via SNI), and DNS traffic. It does **not** perform MITM — TLS connections are tunneled, not decrypted. See [docs/networking.md](docs/networking.md) and [docs/security.md](docs/security.md) for details.

See [examples/sessions/](examples/sessions/) for more session configuration patterns.

## Go SDK

The `pkg/lib` package provides full programmatic access. Use `EngineFake` for testing without real infrastructure.

```go
import "github.com/slok/sbx/pkg/lib"

func main() {
    ctx := context.Background()

    client, _ := lib.New(ctx, lib.Config{
        DBPath: "/tmp/sbx-test.db",
        Engine: lib.EngineFake,
    })
    defer client.Close()

    // Create and start.
    client.CreateSandbox(ctx, lib.CreateSandboxOpts{
        Name:      "my-sandbox",
        Engine:    lib.EngineFake,
        Resources: lib.Resources{VCPUs: 2, MemoryMB: 1024, DiskGB: 10},
    })
    client.StartSandbox(ctx, "my-sandbox", &lib.StartSandboxOpts{
        Env: map[string]string{"APP_ENV": "dev"},
    })

    // Run a command.
    result, _ := client.Exec(ctx, "my-sandbox", []string{"echo", "hello"}, nil)
    fmt.Println("exit:", result.ExitCode)

    // Cleanup.
    client.RemoveSandbox(ctx, "my-sandbox", true)
}
```

See [`pkg/lib/`](pkg/lib/) for the full API and [`pkg/lib/example_test.go`](pkg/lib/example_test.go) for runnable examples.

## Documentation

| Document | Description |
|----------|-------------|
| [Commands Reference](docs/commands.md) | Full CLI reference with all flags and options |
| [Architecture](docs/architecture.md) | Project structure, layers, and design decisions |
| [Development](docs/development.md) | Building, testing, CI/CD, and contributing |
| [Networking](docs/networking.md) | TAP devices, nftables, SSH, port forwarding |
| [Images](docs/images.md) | Image management, releases, and snapshots |
| [Security](docs/security.md) | Egress filtering, proxy architecture, security model |

## License

[MIT](LICENSE)
