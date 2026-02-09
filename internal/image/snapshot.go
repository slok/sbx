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

// LocalSnapshotCreatorConfig configures the local snapshot creator.
type LocalSnapshotCreatorConfig struct {
	// ImagesDir is the local directory for storing images.
	ImagesDir string
	// Logger for logging.
	Logger log.Logger
}

func (c *LocalSnapshotCreatorConfig) defaults() error {
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

// LocalSnapshotCreator implements SnapshotCreator using the local filesystem.
type LocalSnapshotCreator struct {
	imagesDir string
	logger    log.Logger
}

// NewLocalSnapshotCreator creates a new local snapshot creator.
func NewLocalSnapshotCreator(cfg LocalSnapshotCreatorConfig) (*LocalSnapshotCreator, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &LocalSnapshotCreator{
		imagesDir: cfg.ImagesDir,
		logger:    cfg.Logger,
	}, nil
}

func (m *LocalSnapshotCreator) Create(_ context.Context, opts CreateSnapshotOptions) error {
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

	// Copy firecracker binary if available.
	if opts.FirecrackerSrc != "" {
		fcDst := filepath.Join(versionDir, "firecracker")
		if err := copyFile(opts.FirecrackerSrc, fcDst); err != nil {
			m.logger.Infof("Could not copy firecracker binary: %v", err)
		} else {
			if err := os.Chmod(fcDst, 0o755); err != nil {
				return fmt.Errorf("chmod firecracker binary: %w", err)
			}
		}
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

	// Build artifact metadata, inheriting from source manifest if available.
	kernelMeta := kernelJSON{
		File:      kernelFile,
		SizeBytes: kernelInfo.Size(),
	}
	rootfsMeta := rootfsJSON{
		File:      rootfsFile,
		SizeBytes: rootfsInfo.Size(),
	}
	fcMeta := firecrackerJSON{}
	buildMeta := buildJSON{}

	if src := opts.SourceManifest; src != nil {
		if archInfo, ok := src.Artifacts[arch]; ok {
			kernelMeta.Version = archInfo.Kernel.Version
			kernelMeta.Source = archInfo.Kernel.Source
			rootfsMeta.Distro = archInfo.Rootfs.Distro
			rootfsMeta.DistroVersion = archInfo.Rootfs.DistroVersion
			rootfsMeta.Profile = archInfo.Rootfs.Profile
		}
		fcMeta.Version = src.Firecracker.Version
		fcMeta.Source = src.Firecracker.Source
		buildMeta.Date = src.Build.Date
		buildMeta.Commit = src.Build.Commit
	}

	mj := manifestJSON{
		SchemaVersion: model.CurrentSchemaVersion,
		Version:       opts.Name,
		Artifacts: map[string]archArtifactsJSON{
			arch: {
				Kernel: kernelMeta,
				Rootfs: rootfsMeta,
			},
		},
		FC:    fcMeta,
		Build: buildMeta,
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
