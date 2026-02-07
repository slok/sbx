package create

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the create service.
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
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.Create"})
	return nil
}

// Service handles sandbox creation business logic.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new create service.
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

// CreateOptions are the options for creating a sandbox.
type CreateOptions struct {
	Config       model.SandboxConfig
	FromSnapshot string
}

// Create creates a new sandbox.
func (s *Service) Create(ctx context.Context, opts CreateOptions) (*model.Sandbox, error) {
	if opts.FromSnapshot != "" {
		snapshot, err := resolveSnapshotByNameOrID(ctx, s.repo, opts.FromSnapshot)
		if err != nil {
			return nil, fmt.Errorf("could not resolve snapshot %q: %w", opts.FromSnapshot, err)
		}

		if _, err := os.Stat(snapshot.Path); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("snapshot file not found at %q: %w", snapshot.Path, model.ErrNotFound)
			}

			return nil, fmt.Errorf("could not access snapshot file %q: %w", snapshot.Path, err)
		}

		if opts.Config.FirecrackerEngine == nil {
			return nil, fmt.Errorf("snapshot restore requires engine rootfs configuration: %w", model.ErrNotValid)
		}

		opts.Config.FirecrackerEngine.RootFS = snapshot.Path
	}

	// 1. Validate config
	if err := opts.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 2. Check name uniqueness
	_, err := s.repo.GetSandboxByName(ctx, opts.Config.Name)
	if err == nil {
		return nil, fmt.Errorf("sandbox with name %q already exists: %w", opts.Config.Name, model.ErrAlreadyExists)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("could not check name uniqueness: %w", err)
	}

	// 3. Create via engine (engine will generate ID and manage tasks)
	sandbox, err := s.engine.Create(ctx, opts.Config)
	if err != nil {
		return nil, fmt.Errorf("could not create sandbox: %w", err)
	}

	// 4. Save to repository
	if err := s.repo.CreateSandbox(ctx, *sandbox); err != nil {
		return nil, fmt.Errorf("could not save sandbox: %w", err)
	}

	s.logger.Infof("Created sandbox: %s (%s)", sandbox.Name, sandbox.ID)

	return sandbox, nil
}

func resolveSnapshotByNameOrID(ctx context.Context, repo storage.Repository, nameOrID string) (*model.Snapshot, error) {
	snapshot, err := repo.GetSnapshotByName(ctx, nameOrID)
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(nameOrID) {
		snapshot, err = repo.GetSnapshot(ctx, nameOrID)
	}
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("snapshot not found: %s: %w", nameOrID, model.ErrNotFound)
		}

		return nil, fmt.Errorf("could not get snapshot: %w", err)
	}

	return snapshot, nil
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
