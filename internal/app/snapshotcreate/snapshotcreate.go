package snapshotcreate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the snapshot create service.
type ServiceConfig struct {
	Engine     sandbox.Engine
	Repository storage.Repository
	Logger     log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Engine == nil {
		return fmt.Errorf("engine is required")
	}

	if c.Repository == nil {
		return fmt.Errorf("repository is required")
	}

	if c.Logger == nil {
		c.Logger = log.Noop
	}

	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.SnapshotCreate"})
	return nil
}

// Service creates snapshots from sandboxes.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new snapshot create service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		engine: cfg.Engine,
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request represents a snapshot creation request.
type Request struct {
	NameOrID     string
	SnapshotName string
}

// Run creates a snapshot for an existing sandbox.
func (s *Service) Run(ctx context.Context, req Request) (*model.Snapshot, error) {
	if req.SnapshotName != "" {
		if err := model.ValidateSnapshotName(req.SnapshotName); err != nil {
			return nil, fmt.Errorf("invalid snapshot name: %w", err)
		}
	}

	sandbox, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(req.NameOrID) {
		sandbox, err = s.repo.GetSandbox(ctx, req.NameOrID)
	}
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("sandbox not found: %s: %w", req.NameOrID, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not get sandbox: %w", err)
	}

	if sandbox.Status != model.SandboxStatusCreated && sandbox.Status != model.SandboxStatusStopped {
		return nil, fmt.Errorf("cannot snapshot sandbox in status %q (must be created or stopped): %w", sandbox.Status, model.ErrNotValid)
	}

	snapshotName, err := s.resolveSnapshotName(ctx, sandbox.Name, req.SnapshotName)
	if err != nil {
		return nil, err
	}

	snapshotID := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader).String()
	dstPath, err := defaultSnapshotPath(snapshotID)
	if err != nil {
		return nil, fmt.Errorf("could not build snapshot destination path: %w", err)
	}

	virtualSize, allocatedSize, err := s.engine.CreateSnapshot(ctx, sandbox.ID, snapshotID, dstPath)
	if err != nil {
		return nil, fmt.Errorf("could not create engine snapshot: %w", err)
	}

	now := time.Now().UTC()
	snapshot := model.Snapshot{
		ID:                 snapshotID,
		Name:               snapshotName,
		Path:               dstPath,
		SourceSandboxID:    sandbox.ID,
		SourceSandboxName:  sandbox.Name,
		VirtualSizeBytes:   virtualSize,
		AllocatedSizeBytes: allocatedSize,
		CreatedAt:          now,
	}

	if err := s.repo.CreateSnapshot(ctx, snapshot); err != nil {
		if rmErr := os.Remove(snapshot.Path); rmErr != nil {
			s.logger.Warningf("could not remove snapshot file after persistence failure: %v", rmErr)
		}
		return nil, fmt.Errorf("could not persist snapshot: %w", err)
	}

	s.logger.Infof("Created snapshot %s (%s) from sandbox %s", snapshot.Name, snapshot.ID, sandbox.ID)

	return &snapshot, nil
}

func makeDefaultSnapshotName(sandboxName string, now time.Time) string {
	base := sanitizeSnapshotNamePart(sandboxName)
	if base == "" {
		base = "snapshot"
	}

	return fmt.Sprintf("%s-%s", base, now.UTC().Format("20060102-1504"))
}

func (s *Service) resolveSnapshotName(ctx context.Context, sandboxName, requestedName string) (string, error) {
	autoName := requestedName == ""
	name := requestedName
	if autoName {
		name = makeDefaultSnapshotName(sandboxName, time.Now().UTC())
	}

	if err := model.ValidateSnapshotName(name); err != nil {
		return "", fmt.Errorf("invalid snapshot name: %w", err)
	}

	_, err := s.repo.GetSnapshotByName(ctx, name)
	if err == nil {
		if !autoName {
			return "", fmt.Errorf("snapshot with name %q already exists: %w", name, model.ErrAlreadyExists)
		}

		name = fmt.Sprintf("%s-%d", name, time.Now().UTC().Unix())
		if err := model.ValidateSnapshotName(name); err != nil {
			return "", fmt.Errorf("invalid auto-generated snapshot name: %w", err)
		}

		_, err = s.repo.GetSnapshotByName(ctx, name)
		if err == nil {
			return "", fmt.Errorf("snapshot with name %q already exists: %w", name, model.ErrAlreadyExists)
		}
	}

	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return "", fmt.Errorf("could not check snapshot name uniqueness: %w", err)
	}

	return name, nil
}

func sanitizeSnapshotNamePart(raw string) string {
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

func defaultSnapshotPath(snapshotID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not get user home dir: %w", err)
	}

	return filepath.Join(home, ".sbx", "snapshots", fmt.Sprintf("%s.ext4", snapshotID)), nil
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
