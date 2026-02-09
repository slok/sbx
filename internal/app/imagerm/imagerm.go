package imagerm

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
)

// ServiceConfig is the configuration for the image remove service.
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

// Service handles removing image releases.
type Service struct {
	manager     image.ImageManager
	snapshotMgr image.SnapshotManager
	logger      log.Logger
}

// NewService creates a new image remove service.
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

// Request is the remove request parameters.
type Request struct {
	Version string
}

// Run removes an installed image (tries snapshot first, then release).
func (s *Service) Run(ctx context.Context, req Request) error {
	// Try snapshot manager first.
	if s.snapshotMgr != nil {
		exists, err := s.snapshotMgr.Exists(ctx, req.Version)
		if err == nil && exists {
			return s.snapshotMgr.Remove(ctx, req.Version)
		}
	}

	// Fall back to image manager (works for both releases and non-snapshot local dirs).
	if err := s.manager.Remove(ctx, req.Version); err != nil {
		return fmt.Errorf("removing image %s: %w", req.Version, err)
	}
	return nil
}
