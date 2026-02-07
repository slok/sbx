package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

// RepositoryConfig is the configuration for the memory repository.
type RepositoryConfig struct {
	Logger log.Logger
}

func (c *RepositoryConfig) defaults() error {
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "storage.Memory"})
	return nil
}

// Repository is an in-memory implementation of storage.Repository.
type Repository struct {
	sandboxes map[string]model.Sandbox
	snapshots map[string]model.Snapshot
	mu        sync.RWMutex
	logger    log.Logger
}

// NewRepository creates a new memory repository.
func NewRepository(cfg RepositoryConfig) (*Repository, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Repository{
		sandboxes: make(map[string]model.Sandbox),
		snapshots: make(map[string]model.Snapshot),
		logger:    cfg.Logger,
	}, nil
}

// CreateSandbox creates a new sandbox in the repository.
func (r *Repository) CreateSandbox(ctx context.Context, s model.Sandbox) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if ID already exists
	if _, ok := r.sandboxes[s.ID]; ok {
		return fmt.Errorf("sandbox with id %s: %w", s.ID, model.ErrAlreadyExists)
	}

	// Check if name already exists
	for _, existing := range r.sandboxes {
		if existing.Name == s.Name {
			return fmt.Errorf("sandbox with name %s: %w", s.Name, model.ErrAlreadyExists)
		}
	}

	r.sandboxes[s.ID] = s
	r.logger.Debugf("Created sandbox in repository: %s", s.ID)

	return nil
}

// GetSandbox retrieves a sandbox by ID.
func (r *Repository) GetSandbox(ctx context.Context, id string) (*model.Sandbox, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sandbox, ok := r.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
	}

	// Return a copy
	sandboxCopy := sandbox
	return &sandboxCopy, nil
}

// GetSandboxByName retrieves a sandbox by name.
func (r *Repository) GetSandboxByName(ctx context.Context, name string) (*model.Sandbox, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, sandbox := range r.sandboxes {
		if sandbox.Name == name {
			// Return a copy
			sandboxCopy := sandbox
			return &sandboxCopy, nil
		}
	}

	return nil, fmt.Errorf("sandbox with name %s: %w", name, model.ErrNotFound)
}

// ListSandboxes returns all sandboxes.
func (r *Repository) ListSandboxes(ctx context.Context) ([]model.Sandbox, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sandboxes := make([]model.Sandbox, 0, len(r.sandboxes))
	for _, sandbox := range r.sandboxes {
		sandboxes = append(sandboxes, sandbox)
	}

	return sandboxes, nil
}

// UpdateSandbox updates an existing sandbox.
func (r *Repository) UpdateSandbox(ctx context.Context, s model.Sandbox) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sandboxes[s.ID]; !ok {
		return fmt.Errorf("sandbox %s: %w", s.ID, model.ErrNotFound)
	}

	r.sandboxes[s.ID] = s
	r.logger.Debugf("Updated sandbox in repository: %s", s.ID)

	return nil
}

// DeleteSandbox deletes a sandbox.
func (r *Repository) DeleteSandbox(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sandboxes[id]; !ok {
		return fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
	}

	delete(r.sandboxes, id)
	r.logger.Debugf("Deleted sandbox from repository: %s", id)

	return nil
}

// CreateSnapshot creates a new snapshot in the repository.
func (r *Repository) CreateSnapshot(ctx context.Context, s model.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := s.Validate(); err != nil {
		return fmt.Errorf("invalid snapshot: %w", err)
	}

	if _, ok := r.snapshots[s.ID]; ok {
		return fmt.Errorf("snapshot with id %s: %w", s.ID, model.ErrAlreadyExists)
	}

	for _, existing := range r.snapshots {
		if existing.Name == s.Name {
			return fmt.Errorf("snapshot with name %s: %w", s.Name, model.ErrAlreadyExists)
		}
	}

	r.snapshots[s.ID] = s
	r.logger.Debugf("Created snapshot in repository: %s", s.ID)

	return nil
}

// GetSnapshot retrieves a snapshot by ID.
func (r *Repository) GetSnapshot(ctx context.Context, id string) (*model.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot, ok := r.snapshots[id]
	if !ok {
		return nil, fmt.Errorf("snapshot %s: %w", id, model.ErrNotFound)
	}

	snapshotCopy := snapshot
	return &snapshotCopy, nil
}

// GetSnapshotByName retrieves a snapshot by name.
func (r *Repository) GetSnapshotByName(ctx context.Context, name string) (*model.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, snapshot := range r.snapshots {
		if snapshot.Name == name {
			snapshotCopy := snapshot
			return &snapshotCopy, nil
		}
	}

	return nil, fmt.Errorf("snapshot with name %s: %w", name, model.ErrNotFound)
}

// ListSnapshots returns all snapshots.
func (r *Repository) ListSnapshots(ctx context.Context) ([]model.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshots := make([]model.Snapshot, 0, len(r.snapshots))
	for _, snapshot := range r.snapshots {
		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}
