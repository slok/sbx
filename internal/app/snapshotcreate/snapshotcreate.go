package snapshotcreate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the snapshot create service.
type ServiceConfig struct {
	ImageManager    image.ImageManager
	SnapshotCreator image.SnapshotCreator
	Repository      storage.Repository
	Logger          log.Logger
	// DataDir is the base sbx data directory (default: ~/.sbx).
	DataDir string
}

func (c *ServiceConfig) defaults() error {
	if c.ImageManager == nil {
		return fmt.Errorf("image manager is required")
	}
	if c.SnapshotCreator == nil {
		return fmt.Errorf("snapshot creator is required")
	}
	if c.Repository == nil {
		return fmt.Errorf("repository is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	if c.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.SnapshotCreate"})
	return nil
}

// Service creates local snapshot images from sandboxes.
type Service struct {
	imgMgr  image.ImageManager
	snapCrt image.SnapshotCreator
	repo    storage.Repository
	logger  log.Logger
	dataDir string
}

// NewService creates a new snapshot create service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Service{
		imgMgr:  cfg.ImageManager,
		snapCrt: cfg.SnapshotCreator,
		repo:    cfg.Repository,
		logger:  cfg.Logger,
		dataDir: cfg.DataDir,
	}, nil
}

// Request represents a snapshot creation request.
type Request struct {
	NameOrID  string
	ImageName string
}

// Run creates a local snapshot image from an existing sandbox.
func (s *Service) Run(ctx context.Context, req Request) (string, error) {
	if req.ImageName != "" {
		if err := model.ValidateImageName(req.ImageName); err != nil {
			return "", fmt.Errorf("invalid image name: %w", err)
		}
	}

	// Resolve sandbox.
	sb, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(req.NameOrID) {
		sb, err = s.repo.GetSandbox(ctx, req.NameOrID)
	}
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", fmt.Errorf("sandbox not found: %s: %w", req.NameOrID, model.ErrNotFound)
		}
		return "", fmt.Errorf("could not get sandbox: %w", err)
	}

	if sb.Status != model.SandboxStatusStopped {
		return "", fmt.Errorf("cannot snapshot sandbox in status %q (must be stopped): %w", sb.Status, model.ErrNotValid)
	}

	// Resolve image name.
	imgName, err := s.resolveImageName(ctx, sb.Name, req.ImageName)
	if err != nil {
		return "", err
	}

	// Determine rootfs path (the actual VM rootfs, not base image).
	rootfsPath := filepath.Join(s.dataDir, "vms", sb.ID, "rootfs.ext4")

	// Determine kernel path from sandbox config.
	var kernelPath string
	if sb.Config.FirecrackerEngine != nil {
		kernelPath = sb.Config.FirecrackerEngine.KernelImage
	}
	if kernelPath == "" {
		return "", fmt.Errorf("sandbox has no kernel image configured: %w", model.ErrNotValid)
	}

	// Try to detect source image from kernel path.
	sourceImage := detectSourceImage(kernelPath)

	// Read source image manifest if available (to inherit metadata).
	var sourceManifest *model.ImageManifest
	var firecrackerSrc string
	if sourceImage != "" {
		manifest, err := s.imgMgr.GetManifest(ctx, sourceImage)
		if err == nil {
			sourceManifest = manifest
			// Use the firecracker binary from the source image.
			firecrackerSrc = s.imgMgr.FirecrackerPath(sourceImage)
		}
	}

	if err := s.snapCrt.Create(ctx, image.CreateSnapshotOptions{
		Name:              imgName,
		KernelSrc:         kernelPath,
		RootFSSrc:         rootfsPath,
		FirecrackerSrc:    firecrackerSrc,
		SourceSandboxID:   sb.ID,
		SourceSandboxName: sb.Name,
		SourceImage:       sourceImage,

		SourceManifest: sourceManifest,
	}); err != nil {
		return "", fmt.Errorf("could not create image: %w", err)
	}

	s.logger.Infof("Created image %s from sandbox %s (%s)", imgName, sb.Name, sb.ID)
	return imgName, nil
}

func (s *Service) resolveImageName(ctx context.Context, sandboxName, requestedName string) (string, error) {
	autoName := requestedName == ""
	name := requestedName
	if autoName {
		name = makeDefaultImageName(sandboxName, time.Now().UTC())
	}

	if err := model.ValidateImageName(name); err != nil {
		return "", fmt.Errorf("invalid image name: %w", err)
	}

	// Check if name already exists using ImageManager (local read).
	exists, err := s.imgMgr.Exists(ctx, name)
	if err != nil {
		return "", fmt.Errorf("could not check image name uniqueness: %w", err)
	}

	if exists {
		if !autoName {
			return "", fmt.Errorf("image with name %q already exists: %w", name, model.ErrAlreadyExists)
		}
		// Add unix timestamp suffix for auto-generated names.
		name = fmt.Sprintf("%s-%d", name, time.Now().UTC().Unix())
		if err := model.ValidateImageName(name); err != nil {
			return "", fmt.Errorf("invalid auto-generated image name: %w", err)
		}
		exists, err = s.imgMgr.Exists(ctx, name)
		if err != nil {
			return "", fmt.Errorf("could not check image name uniqueness: %w", err)
		}
		if exists {
			return "", fmt.Errorf("image with name %q already exists: %w", name, model.ErrAlreadyExists)
		}
	}

	return name, nil
}

func makeDefaultImageName(sandboxName string, now time.Time) string {
	base := sanitizeImageNamePart(sandboxName)
	if base == "" {
		base = "snapshot"
	}
	return fmt.Sprintf("%s-%s", base, now.UTC().Format("20060102-1504"))
}

func sanitizeImageNamePart(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-._")
}

// detectSourceImage tries to extract the image version from the kernel path.
// E.g., /home/user/.sbx/images/v0.1.0/vmlinux-x86_64 -> "v0.1.0".
func detectSourceImage(kernelPath string) string {
	parts := strings.Split(filepath.ToSlash(kernelPath), "/")
	for i, p := range parts {
		if p == "images" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func looksLikeULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}
