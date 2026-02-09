package image

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	fileutil "github.com/slok/sbx/internal/utils/file"
)

// LocalImageManagerConfig configures the local image manager.
type LocalImageManagerConfig struct {
	// ImagesDir is the local directory for storing images.
	ImagesDir string
	// Logger for logging.
	Logger log.Logger
}

func (c *LocalImageManagerConfig) defaults() error {
	if c.ImagesDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not get user home dir: %w", err)
		}
		c.ImagesDir = filepath.Join(home, DefaultImagesDir)
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	return nil
}

// LocalImageManager implements ImageManager using the local filesystem.
// It handles all locally installed images uniformly (both releases and snapshots).
type LocalImageManager struct {
	imagesDir string
	logger    log.Logger
}

// NewLocalImageManager creates a new local image manager.
func NewLocalImageManager(cfg LocalImageManagerConfig) (*LocalImageManager, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &LocalImageManager{
		imagesDir: cfg.ImagesDir,
		logger:    cfg.Logger,
	}, nil
}

func (m *LocalImageManager) List(_ context.Context) ([]model.ImageRelease, error) {
	entries, err := os.ReadDir(m.imagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading images directory: %w", err)
	}

	var result []model.ImageRelease
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Only include dirs with a valid manifest.
		mj, err := readLocalManifest(m.imagesDir, name)
		if err != nil {
			continue
		}

		source := model.ImageSourceRelease
		if mj.Snapshot != nil {
			source = model.ImageSourceSnapshot
		}

		result = append(result, model.ImageRelease{
			Version:   name,
			Installed: true,
			Source:    source,
		})
	}

	return result, nil
}

func (m *LocalImageManager) GetManifest(_ context.Context, name string) (*model.ImageManifest, error) {
	mj, err := readLocalManifest(m.imagesDir, name)
	if err != nil {
		return nil, fmt.Errorf("reading manifest for %s: %w", name, err)
	}

	if mj.SchemaVersion == 0 {
		mj.SchemaVersion = 1
	}
	if mj.SchemaVersion != model.CurrentSchemaVersion {
		return nil, fmt.Errorf("unsupported manifest schema version %d for %s (supported: %d), try updating sbx",
			mj.SchemaVersion, name, model.CurrentSchemaVersion)
	}

	return mj.toModel(), nil
}

func (m *LocalImageManager) Remove(_ context.Context, name string) error {
	versionDir := filepath.Join(m.imagesDir, name)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("image %s is not installed", name)
	}
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("removing image %s: %w", name, err)
	}
	return nil
}

func (m *LocalImageManager) Exists(_ context.Context, name string) (bool, error) {
	versionDir := filepath.Join(m.imagesDir, name)
	info, err := os.Stat(versionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

func (m *LocalImageManager) KernelPath(name string) string {
	return filepath.Join(m.imagesDir, name, fmt.Sprintf("vmlinux-%s", HostArch()))
}

func (m *LocalImageManager) RootFSPath(name string) string {
	return filepath.Join(m.imagesDir, name, fmt.Sprintf("rootfs-%s.ext4", HostArch()))
}

func (m *LocalImageManager) FirecrackerPath(name string) string {
	return filepath.Join(m.imagesDir, name, "firecracker")
}

// --- Shared helpers (used by LocalImageManager, LocalSnapshotCreator, GitHubImagePuller) ---

// readLocalManifest reads and parses a manifest.json from a local version directory.
func readLocalManifest(imagesDir, version string) (*manifestJSON, error) {
	manifestPath := filepath.Join(imagesDir, version, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var mj manifestJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, fmt.Errorf("parsing local manifest for %s: %w", version, err)
	}

	return &mj, nil
}

// copyFile copies a file from src to dst, preserving sparse holes when possible.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination %s: %w", dst, err)
	}
	defer dstFile.Close()

	// Try sparse-aware copy first, fall back to regular copy.
	if err := fileutil.CopyFileSparse(context.Background(), srcFile, dstFile); err != nil {
		// Reset both files and fall back to regular copy.
		if _, serr := srcFile.Seek(0, io.SeekStart); serr != nil {
			return fmt.Errorf("seeking source: %w", serr)
		}
		if err := dstFile.Truncate(0); err != nil {
			return fmt.Errorf("truncating destination: %w", err)
		}
		if _, serr := dstFile.Seek(0, io.SeekStart); serr != nil {
			return fmt.Errorf("seeking destination: %w", serr)
		}
		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("copying %s to %s: %w", src, dst, err)
		}
	}

	return nil
}
