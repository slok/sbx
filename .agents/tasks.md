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