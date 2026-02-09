package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/sqlite/migrations"
)

// RepositoryConfig is the configuration for the SQLite repository.
type RepositoryConfig struct {
	DBPath string
	Logger log.Logger
}

func (c *RepositoryConfig) defaults() error {
	if c.DBPath == "" {
		return fmt.Errorf("db path is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "storage.SQLite"})
	return nil
}

// Repository is a SQLite implementation of storage.Repository.
type Repository struct {
	db     *sql.DB
	logger log.Logger
}

// NewRepository creates a new SQLite repository.
func NewRepository(ctx context.Context, cfg RepositoryConfig) (*Repository, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	dir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("could not create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", cfg.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	migrator, err := migrations.NewMigrator(db, cfg.Logger)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("could not create migrator: %w", err)
	}
	if err := migrator.Up(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not run migrations: %w", err)
	}

	cfg.Logger.Debugf("SQLite repository initialized at %s", cfg.DBPath)

	return &Repository{db: db, logger: cfg.Logger}, nil
}

// Close closes the database connection.
func (r *Repository) Close() error { return r.db.Close() }

// CreateSandbox creates a new sandbox in the repository.
func (r *Repository) CreateSandbox(ctx context.Context, s model.Sandbox) error {
	if s.Config.FirecrackerEngine == nil {
		return fmt.Errorf("firecracker engine config is required: %w", model.ErrNotValid)
	}

	var startedAt, stoppedAt *int64
	if s.StartedAt != nil {
		u := s.StartedAt.Unix()
		startedAt = &u
	}
	if s.StoppedAt != nil {
		u := s.StoppedAt.Unix()
		stoppedAt = &u
	}

	query := `
		INSERT INTO sandboxes (
			id, name, status,
			rootfs_path, kernel_image_path,
			vcpus, memory_mb, disk_gb,
			internal_ip,
			created_at, started_at, stopped_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(
		ctx,
		query,
		s.ID,
		s.Name,
		s.Status,
		s.Config.FirecrackerEngine.RootFS,
		s.Config.FirecrackerEngine.KernelImage,
		s.Config.Resources.VCPUs,
		s.Config.Resources.MemoryMB,
		s.Config.Resources.DiskGB,
		s.InternalIP,
		s.CreatedAt.Unix(),
		startedAt,
		stoppedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: sandboxes.") {
			return fmt.Errorf("sandbox already exists: %w", model.ErrAlreadyExists)
		}
		return fmt.Errorf("could not insert sandbox: %w", err)
	}

	r.logger.Debugf("Created sandbox in repository: %s", s.ID)
	return nil
}

// GetSandbox retrieves a sandbox by ID.
func (r *Repository) GetSandbox(ctx context.Context, id string) (*model.Sandbox, error) {
	query := `
		SELECT
			id, name, status,
			rootfs_path, kernel_image_path,
			vcpus, memory_mb, disk_gb,
			internal_ip,
			created_at, started_at, stopped_at
		FROM sandboxes
		WHERE id = ?
	`

	sandbox, err := r.scanOne(ctx, query, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not query sandbox: %w", err)
	}

	return sandbox, nil
}

// GetSandboxByName retrieves a sandbox by name.
func (r *Repository) GetSandboxByName(ctx context.Context, name string) (*model.Sandbox, error) {
	query := `
		SELECT
			id, name, status,
			rootfs_path, kernel_image_path,
			vcpus, memory_mb, disk_gb,
			internal_ip,
			created_at, started_at, stopped_at
		FROM sandboxes
		WHERE name = ?
	`

	sandbox, err := r.scanOne(ctx, query, name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sandbox with name %s: %w", name, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not query sandbox: %w", err)
	}

	return sandbox, nil
}

// ListSandboxes returns all sandboxes.
func (r *Repository) ListSandboxes(ctx context.Context) ([]model.Sandbox, error) {
	query := `
		SELECT
			id, name, status,
			rootfs_path, kernel_image_path,
			vcpus, memory_mb, disk_gb,
			internal_ip,
			created_at, started_at, stopped_at
		FROM sandboxes
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("could not query sandboxes: %w", err)
	}
	defer rows.Close()

	var sandboxes []model.Sandbox
	for rows.Next() {
		sandbox, err := r.scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("could not scan row: %w", err)
		}
		sandboxes = append(sandboxes, sandbox)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return sandboxes, nil
}

// UpdateSandbox updates an existing sandbox.
func (r *Repository) UpdateSandbox(ctx context.Context, s model.Sandbox) error {
	if s.Config.FirecrackerEngine == nil {
		return fmt.Errorf("firecracker engine config is required: %w", model.ErrNotValid)
	}

	var startedAt, stoppedAt *int64
	if s.StartedAt != nil {
		u := s.StartedAt.Unix()
		startedAt = &u
	}
	if s.StoppedAt != nil {
		u := s.StoppedAt.Unix()
		stoppedAt = &u
	}

	query := `
		UPDATE sandboxes
		SET
			name = ?,
			status = ?,
			rootfs_path = ?,
			kernel_image_path = ?,
			vcpus = ?,
			memory_mb = ?,
			disk_gb = ?,
			internal_ip = ?,
			created_at = ?,
			started_at = ?,
			stopped_at = ?
		WHERE id = ?
	`

	result, err := r.db.ExecContext(
		ctx,
		query,
		s.Name,
		s.Status,
		s.Config.FirecrackerEngine.RootFS,
		s.Config.FirecrackerEngine.KernelImage,
		s.Config.Resources.VCPUs,
		s.Config.Resources.MemoryMB,
		s.Config.Resources.DiskGB,
		s.InternalIP,
		s.CreatedAt.Unix(),
		startedAt,
		stoppedAt,
		s.ID,
	)
	if err != nil {
		return fmt.Errorf("could not update sandbox: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("sandbox %s: %w", s.ID, model.ErrNotFound)
	}

	r.logger.Debugf("Updated sandbox in repository: %s", s.ID)
	return nil
}

// DeleteSandbox deletes a sandbox.
func (r *Repository) DeleteSandbox(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM sandboxes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("could not delete sandbox: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
	}

	r.logger.Debugf("Deleted sandbox from repository: %s", id)
	return nil
}

func (r *Repository) scanOne(ctx context.Context, query string, arg any) (*model.Sandbox, error) {
	row := r.db.QueryRowContext(ctx, query, arg)
	sandbox, err := r.scanRow(row)
	if err != nil {
		return nil, err
	}
	return &sandbox, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *Repository) scanRow(s scanner) (model.Sandbox, error) {
	var sandbox model.Sandbox
	var rootFSPath, kernelImagePath string
	var vcpus float64
	var memoryMB, diskGB int
	var internalIP string
	var createdAt, startedAt, stoppedAt sql.NullInt64

	err := s.Scan(
		&sandbox.ID,
		&sandbox.Name,
		&sandbox.Status,
		&rootFSPath,
		&kernelImagePath,
		&vcpus,
		&memoryMB,
		&diskGB,
		&internalIP,
		&createdAt,
		&startedAt,
		&stoppedAt,
	)
	if err != nil {
		return model.Sandbox{}, err
	}

	sandbox.Config = model.SandboxConfig{
		Name: sandbox.Name,
		FirecrackerEngine: &model.FirecrackerEngineConfig{
			RootFS:      rootFSPath,
			KernelImage: kernelImagePath,
		},
		Resources: model.Resources{VCPUs: vcpus, MemoryMB: memoryMB, DiskGB: diskGB},
	}
	sandbox.InternalIP = internalIP

	if err := r.setTimestamps(&sandbox, createdAt, startedAt, stoppedAt); err != nil {
		return model.Sandbox{}, err
	}

	return sandbox, nil
}

func (r *Repository) setTimestamps(s *model.Sandbox, createdAt, startedAt, stoppedAt sql.NullInt64) error {
	if !createdAt.Valid {
		return fmt.Errorf("created_at is required")
	}
	s.CreatedAt = timeFromUnix(createdAt.Int64)

	if startedAt.Valid {
		t := timeFromUnix(startedAt.Int64)
		s.StartedAt = &t
	}
	if stoppedAt.Valid {
		t := timeFromUnix(stoppedAt.Int64)
		s.StoppedAt = &t
	}

	return nil
}

func timeFromUnix(unix int64) time.Time { return time.Unix(unix, 0).UTC() }
