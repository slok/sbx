package lib

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdklib "github.com/slok/sbx/pkg/lib"
)

// hostArch returns the Firecracker architecture name for the current host.
// Mirrors internal/image.HostArch() for test use.
func hostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

// Config holds integration test configuration loaded from environment variables.
type Config struct {
	ImageVersion string
	ImagesDir    string
}

func (c *Config) defaults() error {
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
// If the activation env var is not set, the test is skipped.
func NewConfig(t *testing.T) Config {
	t.Helper()

	const (
		envActivation   = "SBX_INTEGRATION"
		envImageVersion = "SBX_INTEGRATION_IMAGE_VERSION"
		envImagesDir    = "SBX_INTEGRATION_IMAGES_DIR"
	)

	if os.Getenv(envActivation) != "true" {
		t.Skipf("Skipping integration test: %s is not set to 'true'", envActivation)
	}

	c := Config{
		ImageVersion: os.Getenv(envImageVersion),
		ImagesDir:    os.Getenv(envImagesDir),
	}

	if err := c.defaults(); err != nil {
		t.Skipf("Skipping due to invalid config: %s", err)
	}

	return c
}

// KernelPath returns the kernel image path for the configured image version.
func (c Config) KernelPath() string {
	return filepath.Join(c.ImagesDir, c.ImageVersion, fmt.Sprintf("vmlinux-%s", hostArch()))
}

// RootFSPath returns the rootfs image path for the configured image version.
func (c Config) RootFSPath() string {
	return filepath.Join(c.ImagesDir, c.ImageVersion, fmt.Sprintf("rootfs-%s.ext4", hostArch()))
}

// FirecrackerBinaryPath returns the firecracker binary path for the configured image version.
func (c Config) FirecrackerBinaryPath() string {
	return filepath.Join(c.ImagesDir, c.ImageVersion, "firecracker")
}

// UniqueName generates a unique sandbox name for test isolation.
func UniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// NewTestClient creates an SDK client with a temp SQLite DB for test isolation.
// The client uses the Firecracker engine with real infrastructure.
func NewTestClient(t *testing.T, config Config) *sdklib.Client {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	client, err := sdklib.New(ctx, sdklib.Config{
		DBPath:  dbPath,
		DataDir: t.TempDir(),
		Engine:  sdklib.EngineFirecracker,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

// CleanupSandbox registers a cleanup function that removes a sandbox forcefully.
func CleanupSandbox(t *testing.T, client *sdklib.Client, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		// Best effort cleanup.
		_, _ = client.RemoveSandbox(ctx, name, true)
	})
}
