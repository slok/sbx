package imagelist

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

// ServiceConfig is the configuration for the image list service.
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

// Service handles listing image releases.
type Service struct {
	manager     image.ImageManager
	snapshotMgr image.SnapshotManager
	logger      log.Logger
}

// NewService creates a new image list service.
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

// Run lists available image releases and local snapshots.
func (s *Service) Run(ctx context.Context) ([]model.ImageRelease, error) {
	releases, err := s.manager.ListReleases(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}

	// Merge snapshot images if snapshot manager is available.
	if s.snapshotMgr != nil {
		snapshots, err := s.snapshotMgr.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing snapshots: %w", err)
		}
		releases = append(releases, snapshots...)
	}

	return releases, nil
}
