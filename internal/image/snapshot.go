package image

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

// LocalSnapshotManagerConfig configures the local snapshot manager.
type LocalSnapshotManagerConfig struct {
	// ImagesDir is the local directory for storing images.
	ImagesDir string
	// Logger for logging.
	Logger log.Logger
}

func (c *LocalSnapshotManagerConfig) defaults() error {
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

// LocalSnapshotManager implements SnapshotManager using the local filesystem.
type LocalSnapshotManager struct {
	imagesDir string
	logger    log.Logger
}

// NewLocalSnapshotManager creates a new local snapshot manager.
func NewLocalSnapshotManager(cfg LocalSnapshotManagerConfig) (*LocalSnapshotManager, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &LocalSnapshotManager{
		imagesDir: cfg.ImagesDir,
		logger:    cfg.Logger,
	}, nil
}

func (m *LocalSnapshotManager) Create(_ context.Context, opts CreateSnapshotOptions) error {
	// Validate name.
	if err := model.ValidateImageName(opts.Name); err != nil {
		return fmt.Errorf("invalid snapshot image name: %w", err)
	}

	// Check name doesn't already exist.
	versionDir := filepath.Join(m.imagesDir, opts.Name)
	if _, err := os.Stat(versionDir); err == nil {
		return fmt.Errorf("image %q already exists: %w", opts.Name, model.ErrAlreadyExists)
	}

	// Create the version directory.
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	// Cleanup on error.
	success := false
	defer func() {
		if !success {
			os.RemoveAll(versionDir)
		}
	}()

	arch := HostArch()
	kernelFile := fmt.Sprintf("vmlinux-%s", arch)
	rootfsFile := fmt.Sprintf("rootfs-%s.ext4", arch)

	// Copy kernel.
	kernelDst := filepath.Join(versionDir, kernelFile)
	if err := copyFile(opts.KernelSrc, kernelDst); err != nil {
		return fmt.Errorf("copying kernel: %w", err)
	}

	// Copy rootfs.
	rootfsDst := filepath.Join(versionDir, rootfsFile)
	if err := copyFile(opts.RootFSSrc, rootfsDst); err != nil {
		return fmt.Errorf("copying rootfs: %w", err)
	}

	// Get file sizes for the manifest.
	kernelInfo, err := os.Stat(kernelDst)
	if err != nil {
		return fmt.Errorf("stat kernel: %w", err)
	}
	rootfsInfo, err := os.Stat(rootfsDst)
	if err != nil {
		return fmt.Errorf("stat rootfs: %w", err)
	}

	// Build and write manifest.
	fcVersion := opts.FirecrackerVersion
	if fcVersion == "" {
		fcVersion = "unknown"
	}

	mj := manifestJSON{
		SchemaVersion: model.CurrentSchemaVersion,
		Version:       opts.Name,
		Artifacts: map[string]archArtifactsJSON{
			arch: {
				Kernel: kernelJSON{
					File:      kernelFile,
					SizeBytes: kernelInfo.Size(),
				},
				Rootfs: rootfsJSON{
					File:      rootfsFile,
					SizeBytes: rootfsInfo.Size(),
				},
			},
		},
		FC: firecrackerJSON{
			Version: fcVersion,
		},
		Build: buildJSON{},
		Snapshot: &snapshotInfoJSON{
			SourceSandboxID:   opts.SourceSandboxID,
			SourceSandboxName: opts.SourceSandboxName,
			SourceImage:       opts.SourceImage,
			ParentSnapshot:    opts.ParentSnapshot,
			CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		},
	}

	manifestData, err := json.MarshalIndent(mj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	manifestPath := filepath.Join(versionDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	success = true
	return nil
}

func (m *LocalSnapshotManager) List(_ context.Context) ([]model.ImageRelease, error) {
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

		// Only include dirs with a snapshot manifest.
		mj, err := readLocalManifest(m.imagesDir, name)
		if err != nil {
			continue
		}
		if mj.Snapshot == nil {
			continue
		}

		result = append(result, model.ImageRelease{
			Version:   name,
			Installed: true,
			Source:    model.ImageSourceSnapshot,
		})
	}

	return result, nil
}

func (m *LocalSnapshotManager) GetManifest(_ context.Context, name string) (*model.ImageManifest, error) {
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

func (m *LocalSnapshotManager) Remove(_ context.Context, name string) error {
	versionDir := filepath.Join(m.imagesDir, name)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("snapshot image %s is not installed", name)
	}
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("removing snapshot image %s: %w", name, err)
	}
	return nil
}

func (m *LocalSnapshotManager) Exists(_ context.Context, name string) (bool, error) {
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

func (m *LocalSnapshotManager) KernelPath(name string) string {
	return filepath.Join(m.imagesDir, name, fmt.Sprintf("vmlinux-%s", HostArch()))
}

func (m *LocalSnapshotManager) RootFSPath(name string) string {
	return filepath.Join(m.imagesDir, name, fmt.Sprintf("rootfs-%s.ext4", HostArch()))
}

func (m *LocalSnapshotManager) FirecrackerPath(name string) string {
	return filepath.Join(m.imagesDir, name, "firecracker")
}
