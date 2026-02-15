# Commands Reference

Full CLI reference for sbx. All commands support `--debug`, `--no-log`, `--no-color`, and `--db-path` global flags.

## Global Flags

| Flag | Default | Env Var | Description |
|------|---------|---------|-------------|
| `--debug` | `false` | | Enable debug logging |
| `--no-log` | `false` | | Disable logger output |
| `--no-color` | `false` | | Disable colored output |
| `--logger` | `default` | | Logger format: `default`, `json` |
| `--db-path` | `~/.sbx/sbx.db` | `SBX_DB_PATH` | SQLite database path |

---

## sbx create

Create a new sandbox. Sandboxes start in `stopped` state.

```bash
sbx create --name my-sandbox --engine firecracker --from-image v0.1.0
sbx create --name my-sandbox --engine firecracker \
  --firecracker-root-fs /path/to/rootfs.ext4 \
  --firecracker-kernel /path/to/vmlinux
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--name` | `-n` | string | | Sandbox name (required) |
| `--engine` | | enum | `firecracker` | Engine: `firecracker`, `fake` |
| `--cpu` | | float | `2` | VCPUs (supports fractional, e.g. `0.5`) |
| `--mem` | | int | `2048` | Memory in MB |
| `--disk` | | int | `10` | Disk in GB |
| `--from-image` | | string | | Use a pulled image version |
| `--firecracker-root-fs` | | string | | Path to rootfs image |
| `--firecracker-kernel` | | string | | Path to kernel image |
| `--images-dir` | | string | `~/.sbx/images` | Local images directory |

`--from-image` and `--firecracker-root-fs`/`--firecracker-kernel` are mutually exclusive.

---

## sbx start

Start a stopped sandbox. Optionally apply session configuration (environment variables, egress policy).

```bash
sbx start my-sandbox
sbx start my-sandbox -f session.yaml --env API_KEY=secret
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--file` | `-f` | string | | Path to session YAML file |
| `--env` | `-e` | string | | `KEY=VALUE` or `KEY` (inherits from host). Repeatable |

**Arguments:** `name-or-id` (required)

When `--env KEY` is used without `=VALUE`, the value is read from the current environment. CLI `--env` flags override values from the session file.

See [Session Configuration](#session-configuration) for the YAML format.

---

## sbx stop

Stop a running sandbox.

```bash
sbx stop my-sandbox
```

**Arguments:** `name-or-id` (required)

---

## sbx rm

Remove a sandbox.

```bash
sbx rm my-sandbox
sbx rm my-sandbox --force   # stops first if running
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Force remove a running sandbox (stops it first) |

**Arguments:** `name-or-id` (required)

---

## sbx list

List all sandboxes.

```bash
sbx list
sbx list --status running
sbx list --format json
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--status` | string | | Filter: `running`, `stopped`, `pending`, `failed` |
| `--format` | enum | `table` | Output: `table`, `json` |

Example table output:

```
ID                           NAME               STATUS     CREATED
01JQYXZ2ABCDEFGH1234567890  example-sandbox    running    2 hours ago
01JQYZ3BCDEFGHIJ2345678901  test-sandbox       stopped    1 day ago
```

---

## sbx status

Show detailed information about a sandbox.

```bash
sbx status my-sandbox
sbx status my-sandbox --format json
sbx status 01JQYXZ2ABCDEFGH1234567890
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | enum | `table` | Output: `table`, `json` |

**Arguments:** `name-or-id` (required)

Example output:

```
Name:       my-sandbox
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

---

## sbx exec

Execute a command inside a running sandbox.

```bash
sbx exec my-sandbox -- cat /etc/os-release
sbx exec my-sandbox -w /workspace -- make build
sbx exec my-sandbox -e MY_VAR=value -- ./script.sh
sbx exec my-sandbox -t -- /bin/bash
sbx exec my-sandbox -f ./config.json -- ./app --config config.json
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--workdir` | `-w` | string | | Working directory inside sandbox |
| `--env` | `-e` | string | | Environment variables. Repeatable |
| `--tty` | `-t` | bool | `false` | Allocate pseudo-TTY |
| `--file` | `-f` | string | | Upload local file before exec. Repeatable |

**Arguments:** `name-or-id` (required), `command...` (required, after `--`)

Files uploaded with `--file` are placed in the working directory (or `/` if no workdir).

---

## sbx shell

Open an interactive shell in a running sandbox. This is a convenience wrapper that runs `/bin/sh` with TTY enabled.

```bash
sbx shell my-sandbox
sbx shell my-sandbox -e MY_VAR=value
sbx shell my-sandbox -f ./setup.sh
```

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--env` | `-e` | string | | Environment variables. Repeatable |
| `--file` | `-f` | string | | Upload local file before shell. Repeatable |

**Arguments:** `name-or-id` (required)

---

## sbx cp

Copy files or directories between host and sandbox. Uses scp-style colon syntax.

```bash
sbx cp ./local-file my-sandbox:/remote/path    # host -> sandbox
sbx cp my-sandbox:/remote/file ./local-path    # sandbox -> host
```

**Arguments:** `source` (required), `destination` (required)

The sandbox name is identified by the colon prefix: `sandbox-name:/path`. One argument must be a local path and the other must use the colon syntax.

---

## sbx forward

Forward local ports to a running sandbox. Blocks until Ctrl+C.

```bash
sbx forward my-sandbox 8080             # localhost:8080 -> sandbox:8080
sbx forward my-sandbox 9000:8080        # localhost:9000 -> sandbox:8080
sbx forward my-sandbox 8080 3000 5432   # multiple ports
sbx forward my-sandbox 8080 --host 0.0.0.0
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--host` | string | `localhost` | Local bind address |

**Arguments:** `name-or-id` (required), `ports...` (required)

Port format: `local:remote` or just `port` (same for both). Uses SSH tunnels for Firecracker sandboxes.

---

## sbx snapshot

Create a snapshot image from a stopped sandbox. The snapshot bundles kernel + rootfs into `~/.sbx/images/<name>/` and can be used with `sbx create --from-image`.

```bash
sbx snapshot my-sandbox --name my-snapshot
sbx snapshot my-sandbox   # auto-generated name: my-sandbox-20260207-0935
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | | Snapshot name (auto-generated if empty) |
| `--images-dir` | string | `~/.sbx/images` | Local images directory |

**Arguments:** `sandbox` (required)

The source sandbox must be in `stopped` state. Snapshot names must be unique across all images and use `[a-zA-Z0-9._-]`.

---

## sbx image list

List available images (both remote releases and local snapshots).

```bash
sbx image list
sbx image list --format json
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | enum | `table` | Output: `table`, `json` |

Shared image flags: `--repo` (default: `slok/sbx-images`), `--images-dir` (default: `~/.sbx/images`).

The `SOURCE` column shows `release` or `snapshot` to distinguish image types.

---

## sbx image pull

Pull a pre-built image release. Downloads kernel, rootfs, and firecracker binary.

```bash
sbx image pull v0.1.0
sbx image pull v0.1.0 --force   # re-download even if installed
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Force re-download |

**Arguments:** `version` (required)

Shared image flags: `--repo`, `--images-dir`.

Images are stored in `~/.sbx/images/<version>/`.

---

## sbx image rm

Remove a locally installed image (release or snapshot).

```bash
sbx image rm v0.1.0
sbx image rm my-snapshot
```

**Arguments:** `version` (required)

Shared image flags: `--images-dir`.

---

## sbx image inspect

Inspect an image manifest.

```bash
sbx image inspect v0.1.0
sbx image inspect my-snapshot --format json
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | enum | `table` | Output: `table`, `json` |

**Arguments:** `version` (required)

Shared image flags: `--images-dir`.

---

## sbx doctor

Run preflight checks for sandbox engines. Verifies KVM access, required binaries, network configuration, etc.

```bash
sbx doctor
sbx doctor --engine firecracker
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--engine` | enum | `all` | Engine to check: `firecracker`, `all` |

---

## Session Configuration

Session files are YAML files passed to `sbx start -f` that configure ephemeral, per-start settings.

```yaml
name: dev-session              # optional label
env:                           # environment variables
  KEY: value
  DATABASE_URL: postgres://localhost/mydb

egress:                        # network egress policy
  default: deny                # "allow" or "deny"
  rules:
    - { domain: "github.com", action: allow }
    - { domain: "*.github.com", action: allow }
    - { domain: "registry.npmjs.org", action: allow }
```

Environment variables are injected into the sandbox and available to all `exec` and `shell` sessions. Egress policies control outbound network access using HTTP/TLS/DNS proxies.

See [examples/sessions/](../examples/sessions/) for more patterns and [networking.md](networking.md) for egress architecture.
