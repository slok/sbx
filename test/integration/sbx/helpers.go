package sbx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/slok/sbx/test/integration/testutils"
)

// Config holds integration test configuration loaded from environment variables.
type Config struct {
	Binary string
	Kernel string
	RootFS string
}

func (c *Config) defaults() error {
	if c.Binary == "" {
		c.Binary = "sbx"
	}

	// If the path is already absolute, just check it exists.
	// If relative, the caller should pass an absolute path via the env var,
	// because go test changes the CWD to the test package directory.
	if !filepath.IsAbs(c.Binary) {
		return fmt.Errorf("SBX_INTEGRATION_BINARY must be an absolute path, got %q", c.Binary)
	}
	if _, err := os.Stat(c.Binary); err != nil {
		return fmt.Errorf("sbx binary not found at %q: %w", c.Binary, err)
	}

	if c.Kernel == "" {
		return fmt.Errorf("kernel image path is required (SBX_INTEGRATION_KERNEL)")
	}
	if _, err := os.Stat(c.Kernel); err != nil {
		return fmt.Errorf("kernel image not found at %q: %w", c.Kernel, err)
	}

	if c.RootFS == "" {
		return fmt.Errorf("rootfs image path is required (SBX_INTEGRATION_ROOTFS)")
	}
	if _, err := os.Stat(c.RootFS); err != nil {
		return fmt.Errorf("rootfs image not found at %q: %w", c.RootFS, err)
	}

	return nil
}

// NewConfig loads integration test configuration from environment variables.
// If the config is invalid or the activation env var is not set, the test is skipped.
func NewConfig(t *testing.T) Config {
	t.Helper()

	const (
		envActivation = "SBX_INTEGRATION"
		envBinary     = "SBX_INTEGRATION_BINARY"
		envKernel     = "SBX_INTEGRATION_KERNEL"
		envRootFS     = "SBX_INTEGRATION_ROOTFS"
	)

	if os.Getenv(envActivation) != "true" {
		t.Skipf("Skipping integration test: %s is not set to 'true'", envActivation)
	}

	c := Config{
		Binary: os.Getenv(envBinary),
		Kernel: os.Getenv(envKernel),
		RootFS: os.Getenv(envRootFS),
	}

	if err := c.defaults(); err != nil {
		t.Skipf("Skipping due to invalid config: %s", err)
	}

	return c
}

// RunSBXCmd runs an sbx command with the given arguments and a specific db path.
// It suppresses logging output for cleaner test output.
// It adds the binary's parent directory's bin/ to PATH so the firecracker binary is discovered.
func RunSBXCmd(ctx context.Context, config Config, dbPath, cmdArgs string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("--no-log --db-path %s %s", dbPath, cmdArgs)

	// Add the project's bin/ directory to PATH so the firecracker engine can find its binary.
	// The sbx binary is at e.g. /project/bin/sbx, so bin/ is its parent dir.
	binDir := filepath.Dir(config.Binary)
	env := []string{
		fmt.Sprintf("PATH=%s:%s", binDir, os.Getenv("PATH")),
	}

	return testutils.RunSBX(ctx, env, config.Binary, args, true)
}

// RunCreate creates a sandbox with the firecracker engine.
func RunCreate(ctx context.Context, config Config, dbPath, name string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("create --name %s --engine firecracker --firecracker-root-fs %s --firecracker-kernel %s --cpu 1 --mem 512 --disk 2",
		name, config.RootFS, config.Kernel)
	return RunSBXCmd(ctx, config, dbPath, args)
}

// RunStart starts a sandbox.
func RunStart(ctx context.Context, config Config, dbPath, name string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("start %s", name))
}

// RunStop stops a sandbox.
func RunStop(ctx context.Context, config Config, dbPath, name string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("stop %s", name))
}

// RunRm removes a sandbox (with force).
func RunRm(ctx context.Context, config Config, dbPath, name string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("rm --force %s", name))
}

// RunList lists sandboxes in JSON format.
func RunList(ctx context.Context, config Config, dbPath string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, "list --format json")
}

// RunExec executes a command in a running sandbox.
// Uses -- separator and passes args as []string to preserve arguments with spaces
// (e.g., sh -c "echo hello > /tmp/file").
func RunExec(ctx context.Context, config Config, dbPath, name string, command []string) (stdout, stderr []byte, err error) {
	// Build args as proper slice to preserve arguments with spaces.
	binDir := filepath.Dir(config.Binary)
	env := []string{
		fmt.Sprintf("PATH=%s:%s", binDir, os.Getenv("PATH")),
	}

	args := []string{"--no-log", "--db-path", dbPath, "exec", name, "--"}
	args = append(args, command...)

	return testutils.RunSBXArgs(ctx, env, config.Binary, args, true)
}

// RunSnapshotCreate creates a snapshot from a sandbox.
func RunSnapshotCreate(ctx context.Context, config Config, dbPath, sandboxName, snapshotName string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("snapshot create %s %s", sandboxName, snapshotName)
	return RunSBXCmd(ctx, config, dbPath, args)
}

// RunSnapshotList lists snapshots in JSON format.
func RunSnapshotList(ctx context.Context, config Config, dbPath string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, "snapshot list --format json")
}

// RunSnapshotRm removes a snapshot.
func RunSnapshotRm(ctx context.Context, config Config, dbPath, nameOrID string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("snapshot rm %s", nameOrID))
}

// RunForward starts port forwarding (blocks until context is cancelled).
func RunForward(ctx context.Context, config Config, dbPath, name, ports string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("forward %s %s", name, ports))
}

func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
