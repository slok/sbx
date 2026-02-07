package snapshotremove

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the snapshot remove service.
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

	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.SnapshotRemove"})
	return nil
}

// Service removes snapshots.
type Service struct {
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new snapshot remove service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request represents the snapshot remove request parameters.
type Request struct {
	// NameOrID is the snapshot name or ID to remove.
	NameOrID string
}

// Run removes a snapshot by name or ID.
// It deletes the snapshot file from disk (best-effort) and removes the DB record.
func (s *Service) Run(ctx context.Context, req Request) (*model.Snapshot, error) {
	s.logger.Debugf("removing snapshot: %s", req.NameOrID)

	// Lookup snapshot by name first, then by ID if it looks like a ULID.
	snapshot, err := s.repo.GetSnapshotByName(ctx, req.NameOrID)
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(req.NameOrID) {
		snapshot, err = s.repo.GetSnapshot(ctx, req.NameOrID)
	}
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("snapshot not found: %s: %w", req.NameOrID, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not get snapshot: %w", err)
	}

	// Remove snapshot file from disk (best-effort: ignore if already gone).
	if err := os.Remove(snapshot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("could not remove snapshot file %s: %w", snapshot.Path, err)
	}

	// Delete from repository.
	if err := s.repo.DeleteSnapshot(ctx, snapshot.ID); err != nil {
		return nil, fmt.Errorf("could not delete snapshot from repository: %w", err)
	}

	s.logger.Infof("removed snapshot: %s (ID: %s)", snapshot.Name, snapshot.ID)
	return snapshot, nil
}

// looksLikeULID checks if a string looks like a ULID (26 characters, alphanumeric uppercase).
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
