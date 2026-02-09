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
	Binary       string
	ImageVersion string
	ImagesDir    string
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

	if c.ImageVersion == "" {
		return fmt.Errorf("image version is required (SBX_INTEGRATION_IMAGE_VERSION)")
	}

	if c.ImagesDir == "" {
		return fmt.Errorf("images directory is required (SBX_INTEGRATION_IMAGES_DIR)")
	}

	// Verify the image is actually pulled.
	imgDir := filepath.Join(c.ImagesDir, c.ImageVersion)
	if _, err := os.Stat(imgDir); err != nil {
		return fmt.Errorf("image %s not found at %q (run 'sbx image pull %s' first): %w", c.ImageVersion, imgDir, c.ImageVersion, err)
	}

	return nil
}

// NewConfig loads integration test configuration from environment variables.
// If the config is invalid or the activation env var is not set, the test is skipped.
func NewConfig(t *testing.T) Config {
	t.Helper()

	const (
		envActivation   = "SBX_INTEGRATION"
		envBinary       = "SBX_INTEGRATION_BINARY"
		envImageVersion = "SBX_INTEGRATION_IMAGE_VERSION"
		envImagesDir    = "SBX_INTEGRATION_IMAGES_DIR"
	)

	if os.Getenv(envActivation) != "true" {
		t.Skipf("Skipping integration test: %s is not set to 'true'", envActivation)
	}

	c := Config{
		Binary:       os.Getenv(envBinary),
		ImageVersion: os.Getenv(envImageVersion),
		ImagesDir:    os.Getenv(envImagesDir),
	}

	if err := c.defaults(); err != nil {
		t.Skipf("Skipping due to invalid config: %s", err)
	}

	return c
}

// FirecrackerDir returns the path to the directory containing the firecracker binary
// from the pulled image. This is added to PATH so start/stop/exec commands can find it.
func (c Config) FirecrackerDir() string {
	return filepath.Join(c.ImagesDir, c.ImageVersion)
}

// RunSBXCmd runs an sbx command with the given arguments and a specific db path.
// It suppresses logging output for cleaner test output.
// It adds the image's directory to PATH so the firecracker binary is discovered by the engine.
func RunSBXCmd(ctx context.Context, config Config, dbPath, cmdArgs string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("--no-log --db-path %s %s", dbPath, cmdArgs)

	// Add the image's directory and the binary's parent dir to PATH
	// so the firecracker engine can find the firecracker binary.
	binDir := filepath.Dir(config.Binary)
	env := []string{
		fmt.Sprintf("PATH=%s:%s:%s", config.FirecrackerDir(), binDir, os.Getenv("PATH")),
	}

	return testutils.RunSBX(ctx, env, config.Binary, args, true)
}

// RunCreate creates a sandbox using the pulled image via --from-image.
func RunCreate(ctx context.Context, config Config, dbPath, name string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("create --name %s --engine firecracker --from-image %s --images-dir %s --cpu 1 --mem 512 --disk 2",
		name, config.ImageVersion, config.ImagesDir)
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
	env := []string{
		fmt.Sprintf("PATH=%s:%s:%s", config.FirecrackerDir(), filepath.Dir(config.Binary), os.Getenv("PATH")),
	}

	args := []string{"--no-log", "--db-path", dbPath, "exec", name, "--"}
	args = append(args, command...)

	return testutils.RunSBXArgs(ctx, env, config.Binary, args, true)
}

// RunSnapshotCreate creates a snapshot image from a sandbox.
// Uses the new top-level `snapshot` command which stores images under --images-dir.
func RunSnapshotCreate(ctx context.Context, config Config, dbPath, sandboxName, snapshotName string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("snapshot %s --name %s --images-dir %s", sandboxName, snapshotName, config.ImagesDir)
	return RunSBXCmd(ctx, config, dbPath, args)
}

// RunForward starts port forwarding (blocks until context is cancelled).
func RunForward(ctx context.Context, config Config, dbPath, name, ports string) (stdout, stderr []byte, err error) {
	return RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("forward %s %s", name, ports))
}

// RunImageList lists image releases in JSON format.
func RunImageList(ctx context.Context, config Config, dbPath, extraArgs string) (stdout, stderr []byte, err error) {
	args := "image list --format json"
	if extraArgs != "" {
		args += " " + extraArgs
	}
	return RunSBXCmd(ctx, config, dbPath, args)
}

// RunImagePull pulls an image version.
func RunImagePull(ctx context.Context, config Config, dbPath, version, extraArgs string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("image pull %s", version)
	if extraArgs != "" {
		args += " " + extraArgs
	}
	return RunSBXCmd(ctx, config, dbPath, args)
}

// RunImageRm removes an installed image version.
func RunImageRm(ctx context.Context, config Config, dbPath, version, extraArgs string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("image rm %s", version)
	if extraArgs != "" {
		args += " " + extraArgs
	}
	return RunSBXCmd(ctx, config, dbPath, args)
}

// RunImageInspect inspects an image version in JSON format.
func RunImageInspect(ctx context.Context, config Config, dbPath, version, extraArgs string) (stdout, stderr []byte, err error) {
	args := fmt.Sprintf("image inspect %s --format json", version)
	if extraArgs != "" {
		args += " " + extraArgs
	}
	return RunSBXCmd(ctx, config, dbPath, args)
}
