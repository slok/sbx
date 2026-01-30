# CI/CD Configuration Summary

## Overview

The sbx project uses **GitHub Actions** for continuous integration and deployment. The CI pipeline runs on every push and pull request to ensure code quality and test coverage.

## Workflow: `.github/workflows/ci.yaml`

### Jobs

#### 1. Check (Code Quality)
- **Runs on**: `ubuntu-latest` with `golangci/golangci-lint:v2.4.0-alpine` container
- **Purpose**: Runs golangci-lint for code quality checks
- **Command**: `./scripts/check/check.sh`
- **Config**: `.golangci.yml`

**Enabled Linters:**
- godot - Comments should end in a period
- misspell - Finds commonly misspelled English words
- revive - Fast, configurable linter
- govet - Go's built-in vet tool
- staticcheck - Go static analysis
- unused - Checks for unused code
- gosimple - Simplify code suggestions
- ineffassign - Detects ineffectual assignments

#### 2. Unit Test
- **Runs on**: `ubuntu-latest`
- **Go Version**: Determined from `go.mod` file
- **Purpose**: Runs all unit tests with race detector and coverage
- **Command**: `make ci-test` → `./scripts/check/unit-test.sh`
- **Flags**: `-race -coverprofile=.test_coverage.txt`
- **Excludes**: Integration tests (`/test/integration`)

**Coverage by Layer:**
- Model: **100%**
- App/Create: **100%**
- Engine/Fake: **96.4%**
- Storage/Memory: **96.1%**
- Storage/SQLite: **76.3%**
- **Total**: ~38.5% (includes infrastructure code)

#### 3. Integration Test
- **Runs on**: `ubuntu-latest`
- **Go Version**: Determined from `go.mod` file
- **Purpose**: Runs end-to-end CLI tests
- **Command**: `make ci-integration-test` → `./scripts/check/integration-test.sh`
- **Tests**: 5 scenarios covering full CLI execution

**Test Scenarios:**
- Successful creation with example config
- Name override works
- Duplicate name detection
- Missing config file handling
- Invalid YAML handling

#### 4. Build
- **Runs on**: `ubuntu-latest`
- **Depends on**: check, unit-test, integration-test
- **Purpose**: Builds the binary and uploads as artifact
- **Artifact**: `sbx-binary` (retained for 7 days)
- **Command**: `make build`

#### 5. Tagged Release Binaries
- **Trigger**: Only on Git tags (`refs/tags/*`)
- **Runs on**: `ubuntu-latest`
- **Depends on**: check, unit-test, integration-test
- **Purpose**: Creates GitHub releases with binaries
- **Permissions**: `contents: write`
- **Output**: Draft release with `bin/*` attached

## Local Testing

You can run the same CI checks locally:

```bash
# Unit tests with coverage
make ci-test

# Integration tests
make ci-integration-test

# Code quality checks (requires golangci-lint)
make ci-check

# Or use individual targets
make test              # Unit tests
make test-integration  # Integration tests
make test-all          # Both
```

## Scripts

All CI scripts are located in `scripts/check/`:

### `check.sh`
```bash
golangci-lint run
```

### `unit-test.sh`
```bash
go test -race -coverprofile=.test_coverage.txt $(go list ./... | grep -v /test/integration)
go tool cover -func=.test_coverage.txt | tail -n1
```

### `integration-test.sh`
```bash
go test -v ./test/integration/...
```

## Configuration Files

- **`.github/workflows/ci.yaml`**: GitHub Actions workflow definition
- **`.golangci.yml`**: golangci-lint configuration
- **`scripts/check/*.sh`**: Executable CI scripts
- **`Makefile`**: Build and test targets

## Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the sbx binary |
| `make test` | Run unit tests locally |
| `make test-integration` | Run integration tests locally |
| `make test-all` | Run all tests |
| `make ci-test` | Run unit tests (CI mode) |
| `make ci-integration-test` | Run integration tests (CI mode) |
| `make ci-check` | Run linters (CI mode) |
| `make go-gen` | Generate mocks |
| `make check` | Run basic checks (vet, fmt) |

## Release Process

To create a new release:

1. Tag the commit:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

2. GitHub Actions will automatically:
   - Run all checks and tests
   - Build the binary
   - Create a draft release
   - Attach the binary to the release

3. Edit the draft release on GitHub to add release notes, then publish

## Current Status

✅ All CI jobs configured and working
✅ Unit tests: 58 test cases across all layers
✅ Integration tests: 5 end-to-end scenarios
✅ Code quality checks with golangci-lint
✅ Automated binary builds
✅ Tagged release automation

**Note**: Docker image building is not included as it's not needed for this project yet.
