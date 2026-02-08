package lib

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/sandbox/firecracker"
	"github.com/slok/sbx/internal/storage"
	"github.com/slok/sbx/internal/storage/sqlite"
)

const (
	defaultDataDir = ".sbx"
	defaultDBFile  = "sbx.db"
)

// Config configures the SDK client.
type Config struct {
	// DBPath is the SQLite database path. Default: ~/.sbx/sbx.db.
	DBPath string
	// DataDir is the base data directory. Default: ~/.sbx.
	DataDir string
	// Logger is optional. Default: noop logger.
	Logger log.Logger
	// Engine forces all sandbox operations to use this engine type.
	// When empty (default), the engine is auto-detected from the stored sandbox
	// configuration (Firecracker config present -> Firecracker engine).
	// Set this to EngineFake for testing without real infrastructure.
	Engine EngineType
}

func (c *Config) defaults() error {
	if c.DataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not get user home dir: %w", err)
		}
		c.DataDir = filepath.Join(home, defaultDataDir)
	}

	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.DataDir, defaultDBFile)
	}

	if c.Logger == nil {
		c.Logger = log.Noop
	}

	return nil
}

// Client is the main SDK entry point for managing sandboxes programmatically.
type Client struct {
	repo       storage.Repository
	logger     log.Logger
	dataDir    string
	engineType EngineType
	closeFn    func() error
}

// New creates a new SDK client. The caller must call Close when done.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: cfg.DBPath,
		Logger: cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create repository: %w", err)
	}

	return &Client{
		repo:       repo,
		logger:     cfg.Logger,
		dataDir:    cfg.DataDir,
		engineType: cfg.Engine,
		closeFn:    repo.Close,
	}, nil
}

// Close releases resources held by the client (e.g. database connection).
func (c *Client) Close() error {
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

// newEngine creates the engine for sandbox operations.
//
// If the client has an explicit engine type set (via Config.Engine), that engine
// is always used. Otherwise, the engine is auto-detected from the sandbox config:
// Firecracker config present -> Firecracker engine, else fake engine.
func (c *Client) newEngine(cfg model.SandboxConfig) (sandbox.Engine, error) {
	engineType := c.resolveEngineType(cfg)

	switch engineType {
	case EngineFirecracker:
		return firecracker.NewEngine(firecracker.EngineConfig{
			DataDir:    c.dataDir,
			Repository: c.repo,
			Logger:     c.logger,
		})
	case EngineFake:
		return fake.NewEngine(fake.EngineConfig{
			Logger: c.logger,
		})
	default:
		return nil, fmt.Errorf("unsupported engine type: %s: %w", engineType, ErrNotValid)
	}
}

// newEngineForCreate creates the engine for sandbox creation with optional binary path.
func (c *Client) newEngineForCreate(engineType EngineType, fcBinary string) (sandbox.Engine, error) {
	switch engineType {
	case EngineFirecracker:
		return firecracker.NewEngine(firecracker.EngineConfig{
			DataDir:           c.dataDir,
			FirecrackerBinary: fcBinary,
			Repository:        c.repo,
			Logger:            c.logger,
		})
	case EngineFake:
		return fake.NewEngine(fake.EngineConfig{
			Logger: c.logger,
		})
	default:
		return nil, fmt.Errorf("unsupported engine type: %s: %w", engineType, ErrNotValid)
	}
}

// resolveEngineType determines which engine to use for a sandbox.
func (c *Client) resolveEngineType(cfg model.SandboxConfig) EngineType {
	// Explicit client-level override takes precedence.
	if c.engineType != "" {
		return c.engineType
	}

	// Auto-detect from sandbox config.
	if cfg.FirecrackerEngine != nil {
		return EngineFirecracker
	}

	return EngineFake
}
