package snapshotlist

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the snapshot list service.
type ServiceConfig struct {
	Repository storage.Repository
	Logger     log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Repository == nil {
		return fmt.Errorf("repository is required")
	}

	if c.Logger == nil {
		c.Logger = log.Noop
	}

	return nil
}

// Service lists snapshots.
type Service struct {
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new snapshot list service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request represents the snapshot list request parameters.
type Request struct{}

// Run lists all snapshots.
func (s *Service) Run(ctx context.Context, req Request) ([]model.Snapshot, error) {
	s.logger.Debugf("listing snapshots")

	snapshots, err := s.repo.ListSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list snapshots: %w", err)
	}

	s.logger.Debugf("found %d snapshots", len(snapshots))
	return snapshots, nil
}
