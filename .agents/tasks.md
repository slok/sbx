# SBX - Tasks

**Purpose:** Guide for AI agents working on SBX. Follow these instructions throughout the project lifecycle.

---

## Workflow

### Task Lifecycle

1. **Read task** — Understand the current task from this file
2. **Implement** — Write code and tests
3. **Test locally** — Run unit tests, iterate until green
4. **Create PR** — Push branch, open pull request
5. **Wait for CI** — Integration tests run in CI
6. **Fix if needed** — Iterate on failures
7. **Merge** — Once CI passes
8. **Mark as done** — Mark as done at the end of this file. 
9. **Stop** — Wait for next task to be added

### Rules

- **Tasks place** — Check `tasks` directory at the root of the project.
- **Dont repeat tasks** — Check at the end of this file the tasks done.
- **One task at a time** — Do not start the next task until current is merged
- **Tests are mandatory** — No PR without tests
- **CI must pass** — Do not merge with failing CI
- **Iterate on failures** — Fix and push until green
- **Know when to stop** — If stuck on the same issue for 15+ iterations, stop and report the blocker

### Pull Request Requirements

Every PR must include:

- **Description** — What this PR does, why it's needed
- **Key decisions** — Important implementation choices made and why
- **Trade-offs** — What alternatives were considered, why this approach won
- **Testing** — How it was tested, what the tests cover

Example PR description:

```markdown
## What

Implements storage interface with SQLite and memory backends.

## Key Decisions

- **Pure Go SQLite** — Chose `modernc.org/sqlite` over `mattn/go-sqlite3` to avoid CGO. 
  Trade-off: Slightly slower but simpler builds and cross-compilation.

- **Repository pattern** — Storage interface returns domain models, not DB rows.
  Keeps business logic decoupled from storage implementation.

## Testing

- Unit tests for memory storage
- Unit tests for SQLite storage with temp file
- Integration test for CLI create command
```

---

## Testing Strategy

### Local (Unit Tests)

- Run frequently during development
- Fast feedback loop — seconds, not minutes
- Mock external dependencies
- Use memory storage and fake engine implementations

### CI (Integration Tests)

- Run on PR
- Can use real dependencies (SQLite files, etc.)
- Slower but more thorough

### Test-Driven Iteration

1. Write/run tests
2. If red: fix code, go to 1
3. If green: commit and push
4. If CI fails: fix, go to 1

---

## Stuck Protocol

If you hit the same error or issue repeatedly:

| Iterations | Action |
|------------|--------|
| 1-5 | Normal debugging, keep trying |
| 6-10 | Step back, try different approach |
| 11-14 | Simplify, remove complexity, check assumptions |
| 15+ | **STOP** — Report blocker, wait for human input |

When stopping, provide:

- What you tried
- The specific error or blocker
- Your hypothesis on root cause
 
---

## Done tasks

- **Task 0001**: Foundation (PR #1) - Merged on 2026-01-30
  - Core SBX implementation: domain models, storage layer (SQLite + memory), engine interface with fake implementation
  - CLI `create` command with YAML config parsing
  - Comprehensive test suite: 58 unit tests + 5 integration test scenarios
  - CI/CD pipeline with GitHub Actions
  - Coverage: Model (100%), App/Create (100%), Engine/Fake (96.4%), Storage/Memory (96.1%), Storage/SQLite (76.3%)

- **Task 0002**: Lifecycle Commands (PR #4) - Merged on 2026-01-30
  - Implemented sandbox lifecycle CLI commands: list, status, stop, start, rm
  - Created printer abstraction layer with table and JSON output formatters
  - Added 5 app services (list, status, start, stop, remove) with full business logic
  - 46 new unit tests with 100% coverage on new services and printer package
  - 21 integration test scenarios covering full sandbox lifecycle
  - Commands support filtering (list --status), force operations (rm --force), and flexible output formats
  - Enhanced README with comprehensive command documentation and usage examples
  - Modified fake engine to be stateless-friendly for integration testing

- **Task 0003**: Docker Engine (PR #5) - Merged on 2026-01-31
  - Implemented full Docker engine with create, start, stop, remove, and status operations
  - Updated domain model with engine-based config (DockerEngine/FirecrackerEngine)
  - YAML config loader updated to parse new engine block format
  - SQLite schema migration added container_id column
  - Fixed CLI commands (stop/start/rm) to use correct engine based on sandbox config
  - All integration tests updated and passing with real Docker containers
  - Config format changed from `base: ubuntu:22.04` to `engine: { docker: { image: ubuntu:22.04 } }`
  - README updated with new configuration format and requirements

- **Task 0004**: Task System (PR #8) - Merged on 2026-01-31
  - Implemented task tracking for multi-step operations with crash recovery
  - Added tasks table (migration 000003) without FK constraint for flexibility
  - Created task.Manager interface with SQLite implementation
  - Integrated into Docker engine (create/start/stop/remove) with progress logging
  - Added optional task tracking to fake engine
  - 8 unit test functions (23 scenarios), 3 integration tests (4 scenarios)
  - Coverage: Task SQLite manager (100%), Docker engine task integration (covered)
  - Design decisions: auto-increment sequences, no auto-cleanup, auto-resume on crash

- **Task 0005**: Exec Command (PR #9) - Merged on 2026-01-31
  - Implemented command execution inside running sandboxes (essential for agentic workflows)
  - Added Exec method to Engine interface with ExecOpts (workdir, env, stdin/stdout/stderr, TTY) and ExecResult (exit code)
  - Docker engine implementation using os/exec with real-time streaming and proper exit code propagation
  - Fake engine exec implementation for unit tests with validation
  - Created sbx exec CLI command with --workdir, --env, and --tty flags
  - Created sbx shell CLI command as convenience wrapper for interactive shells (/bin/sh with TTY)
  - 12 unit tests with 100% coverage + integration tests

- **Task 0006**: Firecracker Engine (PR #10) - Merged on 2026-01-31
  - Implemented Firecracker microVM engine for secure sandboxes with ~125ms boot time
  - Added Check() method to Engine interface for preflight checks
  - Full lifecycle operations: create, start, stop, remove, exec via SSH
  - SSH key management for VM access with auto-generation
  - TAP network + iptables NAT for outbound connectivity
  - Hash-based IP/MAC allocation for deterministic addressing
  - Task system integration for crash recovery
  - Doctor CLI command (sbx doctor) for engine health checks
  - 40+ unit tests covering all components
  - Requirements: KVM access, root/CAP_NET_ADMIN for TAP/iptables

- **Task 0007**: Firecracker Restart Support (PR #11) - Merged on 2026-02-01
  - Implemented `sbx start` for stopped Firecracker sandboxes by respawning VM process
  - Added Repository to Firecracker EngineConfig for reading sandbox config on start
  - Transparent restart: preserves rootfs (disk state), IP address, sandbox ID
  - Auto-recreates TAP device and iptables rules if missing (e.g., after system reboot)
  - Network configuration applied on start (IP, route, DNS via SSH)
  - 5 unit tests with table-driven pattern covering error cases
  - New process PID updated in repository after successful start

- **Task 0008**: Copy Command (PR #13) - Merged on 2026-02-01
  - Implemented `sbx cp` command for copying files/directories between host and sandboxes
  - Added CopyTo/CopyFrom methods to Engine interface
  - Docker engine uses `docker cp` command for file transfer
  - Firecracker engine uses `scp -r` over SSH with sandbox IP
  - Colon syntax like scp: `sbx cp ./file sandbox:/path` or `sbx cp sandbox:/path ./file`
  - Direction auto-detected from argument syntax (which has the colon prefix)
  - App service validates source exists (for host sources) and sandbox is running
  - 26 unit tests + 5 integration test scenarios
  - Fake engine implementation for testing with validation

- **Task 0009**: Port Forward Command (PR #14) - Merged on 2026-02-02
  - Implemented `sbx forward` command for forwarding ports from localhost to running sandboxes
  - Added Forward method to Engine interface with PortMapping model
  - Firecracker engine uses SSH tunnels (`ssh -N -L`) for dynamic port forwarding
  - Docker engine returns clear error (ports must be defined at container creation)
  - Port mapping parsing supports short (`8080`) and full (`9000:8080`) formats
  - Validates port range (1-65535) and sandbox state (must be running)
  - Graceful shutdown on Ctrl+C via context cancellation
  - 30 unit tests covering parsing, service validation, and engine behavior
  - Usage: `sbx forward my-sandbox 8080` or `sbx forward my-sandbox 8080:8080 3000`

- **Task 0010**: Rootfs Resize (PR #15) - Merged on 2026-02-02
  - Automatic rootfs resizing for Firecracker sandboxes using `disk_gb` config
  - Two-step resize: host-side file extension via `os.Truncate()`, guest-side filesystem expansion via `resize2fs` over SSH
  - Added `resize_rootfs` and `expand_filesystem` tasks to create flow (now 11 tasks)
  - 25GB hard limit on disk size with early validation
  - Base image protection: error if `disk_gb < base image size`
  - 13 unit tests covering resize, validation, and error cases
  - No integration tests (unit tests only as requested)