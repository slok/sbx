package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	// Ensure directory exists
	dir := filepath.Dir(cfg.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("could not create db directory: %w", err)
	}

	// Open SQLite with WAL mode and foreign keys enabled
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", cfg.DBPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}

	// Run migrations
	migrator, err := migrations.NewMigrator(db, cfg.Logger)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("could not create migrator: %w", err)
	}
	if err := migrator.Up(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not run migrations: %w", err)
	}

	cfg.Logger.Infof("SQLite repository initialized at %s", cfg.DBPath)

	return &Repository{
		db:     db,
		logger: cfg.Logger,
	}, nil
}

// Close closes the database connection.
func (r *Repository) Close() error {
	return r.db.Close()
}

// DB returns the underlying database connection.
func (r *Repository) DB() *sql.DB {
	return r.db
}

// CreateSandbox creates a new sandbox in the repository.
func (r *Repository) CreateSandbox(ctx context.Context, s model.Sandbox) error {
	// Marshal config to JSON
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}

	// Convert nullable times to SQL-friendly format
	var startedAt, stoppedAt *int64
	if s.StartedAt != nil {
		unix := s.StartedAt.Unix()
		startedAt = &unix
	}
	if s.StoppedAt != nil {
		unix := s.StoppedAt.Unix()
		stoppedAt = &unix
	}

	query := `
		INSERT INTO sandboxes (id, name, status, config_json, container_id, created_at, started_at, stopped_at, error, pid, socket_path, tap_device, internal_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.ExecContext(ctx, query,
		s.ID,
		s.Name,
		s.Status,
		string(configJSON),
		s.ContainerID,
		s.CreatedAt.Unix(),
		startedAt,
		stoppedAt,
		s.Error,
		s.PID,
		s.SocketPath,
		s.TapDevice,
		s.InternalIP,
	)
	if err != nil {
		// Check for unique constraint violation
		if err.Error() == "UNIQUE constraint failed: sandboxes.id" ||
			err.Error() == "UNIQUE constraint failed: sandboxes.name" {
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
		SELECT id, name, status, config_json, container_id, created_at, started_at, stopped_at, error, pid, socket_path, tap_device, internal_ip
		FROM sandboxes
		WHERE id = ?
	`

	var sandbox model.Sandbox
	var configJSON string
	var createdAt, startedAt, stoppedAt sql.NullInt64
	var pid sql.NullInt64
	var socketPath, tapDevice, internalIP sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&sandbox.ID,
		&sandbox.Name,
		&sandbox.Status,
		&configJSON,
		&sandbox.ContainerID,
		&createdAt,
		&startedAt,
		&stoppedAt,
		&sandbox.Error,
		&pid,
		&socketPath,
		&tapDevice,
		&internalIP,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not query sandbox: %w", err)
	}

	// Unmarshal config
	if err := json.Unmarshal([]byte(configJSON), &sandbox.Config); err != nil {
		return nil, fmt.Errorf("could not unmarshal config: %w", err)
	}

	// Convert timestamps
	if err := r.setTimestamps(&sandbox, createdAt, startedAt, stoppedAt); err != nil {
		return nil, err
	}

	// Set Firecracker fields
	if pid.Valid {
		sandbox.PID = int(pid.Int64)
	}
	if socketPath.Valid {
		sandbox.SocketPath = socketPath.String
	}
	if tapDevice.Valid {
		sandbox.TapDevice = tapDevice.String
	}
	if internalIP.Valid {
		sandbox.InternalIP = internalIP.String
	}

	return &sandbox, nil
}

// GetSandboxByName retrieves a sandbox by name.
func (r *Repository) GetSandboxByName(ctx context.Context, name string) (*model.Sandbox, error) {
	query := `
		SELECT id, name, status, config_json, container_id, created_at, started_at, stopped_at, error, pid, socket_path, tap_device, internal_ip
		FROM sandboxes
		WHERE name = ?
	`

	var sandbox model.Sandbox
	var configJSON string
	var createdAt, startedAt, stoppedAt sql.NullInt64
	var pid sql.NullInt64
	var socketPath, tapDevice, internalIP sql.NullString

	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&sandbox.ID,
		&sandbox.Name,
		&sandbox.Status,
		&configJSON,
		&sandbox.ContainerID,
		&createdAt,
		&startedAt,
		&stoppedAt,
		&sandbox.Error,
		&pid,
		&socketPath,
		&tapDevice,
		&internalIP,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sandbox with name %s: %w", name, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not query sandbox: %w", err)
	}

	// Unmarshal config
	if err := json.Unmarshal([]byte(configJSON), &sandbox.Config); err != nil {
		return nil, fmt.Errorf("could not unmarshal config: %w", err)
	}

	// Convert timestamps
	if err := r.setTimestamps(&sandbox, createdAt, startedAt, stoppedAt); err != nil {
		return nil, err
	}

	// Set Firecracker fields
	if pid.Valid {
		sandbox.PID = int(pid.Int64)
	}
	if socketPath.Valid {
		sandbox.SocketPath = socketPath.String
	}
	if tapDevice.Valid {
		sandbox.TapDevice = tapDevice.String
	}
	if internalIP.Valid {
		sandbox.InternalIP = internalIP.String
	}

	return &sandbox, nil
}

// ListSandboxes returns all sandboxes.
func (r *Repository) ListSandboxes(ctx context.Context) ([]model.Sandbox, error) {
	query := `
		SELECT id, name, status, config_json, container_id, created_at, started_at, stopped_at, error, pid, socket_path, tap_device, internal_ip
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
		var sandbox model.Sandbox
		var configJSON string
		var createdAt, startedAt, stoppedAt sql.NullInt64
		var pid sql.NullInt64
		var socketPath, tapDevice, internalIP sql.NullString

		err := rows.Scan(
			&sandbox.ID,
			&sandbox.Name,
			&sandbox.Status,
			&configJSON,
			&sandbox.ContainerID,
			&createdAt,
			&startedAt,
			&stoppedAt,
			&sandbox.Error,
			&pid,
			&socketPath,
			&tapDevice,
			&internalIP,
		)
		if err != nil {
			return nil, fmt.Errorf("could not scan row: %w", err)
		}

		// Unmarshal config
		if err := json.Unmarshal([]byte(configJSON), &sandbox.Config); err != nil {
			return nil, fmt.Errorf("could not unmarshal config: %w", err)
		}

		// Convert timestamps
		if err := r.setTimestamps(&sandbox, createdAt, startedAt, stoppedAt); err != nil {
			return nil, err
		}

		// Set Firecracker fields
		if pid.Valid {
			sandbox.PID = int(pid.Int64)
		}
		if socketPath.Valid {
			sandbox.SocketPath = socketPath.String
		}
		if tapDevice.Valid {
			sandbox.TapDevice = tapDevice.String
		}
		if internalIP.Valid {
			sandbox.InternalIP = internalIP.String
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
	// Marshal config to JSON
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}

	// Convert nullable times
	var startedAt, stoppedAt *int64
	if s.StartedAt != nil {
		unix := s.StartedAt.Unix()
		startedAt = &unix
	}
	if s.StoppedAt != nil {
		unix := s.StoppedAt.Unix()
		stoppedAt = &unix
	}

	query := `
		UPDATE sandboxes
		SET name = ?, status = ?, config_json = ?, container_id = ?, created_at = ?, started_at = ?, stopped_at = ?, error = ?, pid = ?, socket_path = ?, tap_device = ?, internal_ip = ?
		WHERE id = ?
	`

	result, err := r.db.ExecContext(ctx, query,
		s.Name,
		s.Status,
		string(configJSON),
		s.ContainerID,
		s.CreatedAt.Unix(),
		startedAt,
		stoppedAt,
		s.Error,
		s.PID,
		s.SocketPath,
		s.TapDevice,
		s.InternalIP,
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
	query := `DELETE FROM sandboxes WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, id)
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

// setTimestamps is a helper to convert SQL timestamps to Go time.Time.
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

// timeFromUnix converts a Unix timestamp to UTC time.
func timeFromUnix(unix int64) time.Time {
	return time.Unix(unix, 0).UTC()
}
