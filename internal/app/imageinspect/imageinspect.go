package imageinspect

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

// ServiceConfig is the configuration for the image inspect service.
type ServiceConfig struct {
	Manager         image.ImageManager
	SnapshotManager image.SnapshotManager
	Logger          log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Manager == nil {
		return fmt.Errorf("image manager is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	return nil
}

// Service handles inspecting image release manifests.
type Service struct {
	manager     image.ImageManager
	snapshotMgr image.SnapshotManager
	logger      log.Logger
}

// NewService creates a new image inspect service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Service{
		manager:     cfg.Manager,
		snapshotMgr: cfg.SnapshotManager,
		logger:      cfg.Logger,
	}, nil
}

// Request is the inspect request parameters.
type Request struct {
	Version string
}

// Run retrieves the manifest for an image (snapshot first, then remote release).
func (s *Service) Run(ctx context.Context, req Request) (*model.ImageManifest, error) {
	// Try snapshot manager first (local snapshots).
	if s.snapshotMgr != nil {
		exists, err := s.snapshotMgr.Exists(ctx, req.Version)
		if err == nil && exists {
			manifest, err := s.snapshotMgr.GetManifest(ctx, req.Version)
			if err == nil {
				return manifest, nil
			}
		}
	}

	// Fall back to remote image manager.
	manifest, err := s.manager.GetManifest(ctx, req.Version)
	if err != nil {
		return nil, fmt.Errorf("inspecting image %s: %w", req.Version, err)
	}
	return manifest, nil
}
