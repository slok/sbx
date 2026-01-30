package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/slok/sbx/internal/log"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

// Migrator handles database migrations for SQLite.
type Migrator struct {
	db     *sql.DB
	logger log.Logger
}

// NewMigrator creates a new migrator instance.
func NewMigrator(db *sql.DB, logger log.Logger) (*Migrator, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	if logger == nil {
		logger = log.Noop
	}

	return &Migrator{
		db:     db,
		logger: logger,
	}, nil
}

// Up runs all available migrations.
func (m *Migrator) Up(ctx context.Context) error {
	inst, close, err := m.instance(ctx)
	defer close()
	if err != nil {
		return err
	}

	err = inst.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("could not run migrations: %w", err)
	}

	m.logger.Debugf("Migrations applied successfully")
	return nil
}

// Down reverts all migrations.
func (m *Migrator) Down(ctx context.Context) error {
	inst, close, err := m.instance(ctx)
	defer close()
	if err != nil {
		return err
	}

	err = inst.Down()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("could not revert migrations: %w", err)
	}

	m.logger.Debugf("Migrations reverted successfully")
	return nil
}

// instance creates a migrate instance with embedded migrations.
func (m *Migrator) instance(ctx context.Context) (instance *migrate.Migrate, close func(), err error) {
	close = func() {}

	driver, err := sqlite3.WithInstance(m.db, &sqlite3.Config{})
	if err != nil {
		return nil, close, fmt.Errorf("could not create driver: %w", err)
	}

	src, err := iofs.New(migrationFiles, "sql")
	if err != nil {
		return nil, close, fmt.Errorf("could not create fs: %w", err)
	}
	close = func() {
		err := src.Close()
		if err != nil {
			m.logger.Errorf("could not close fs: %s", err)
		}
	}

	instance, err = migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		return nil, close, fmt.Errorf("could not create migration instance: %w", err)
	}

	return instance, close, nil
}
