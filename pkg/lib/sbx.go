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
//
// All fields are optional and have sensible defaults. At minimum, an empty
// Config{} will use ~/.sbx/sbx.db for storage and auto-detect the engine.
type Config struct {
	// DBPath is the SQLite database path.
	// Default: ~/.sbx/sbx.db.
	DBPath string

	// DataDir is the base directory for sbx data (VMs, snapshots, SSH keys).
	// Default: ~/.sbx.
	DataDir string

	// Logger receives structured log output from the SDK.
	// Default: noop (silent). See the log sub-package for the interface.
	Logger log.Logger

	// Engine forces all sandbox operations to use this engine type.
	// When empty (default), the engine is auto-detected from the stored
	// sandbox configuration (Firecracker config present -> Firecracker engine).
	//
	// Set this to [EngineFake] for testing without real infrastructure.
	Engine EngineType

	// FirecrackerBinary is the path to the firecracker binary.
	// If empty, the binary is searched in ./bin/ and PATH.
	// Only used when Engine is [EngineFirecracker].
	FirecrackerBinary string

	// ImagesDir is the directory for downloaded images (kernel, rootfs, firecracker).
	// Default: ~/.sbx/images.
	ImagesDir string

	// ImageRepo is the GitHub repository for image releases.
	// Default: "slok/sbx-images".
	ImageRepo string
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

	if c.ImagesDir == "" {
		c.ImagesDir = filepath.Join(c.DataDir, "images")
	}

	return nil
}

// Client is the main SDK entry point for managing sandboxes programmatically.
//
// Create a Client with [New] and release its resources with [Client.Close].
// A Client is safe for concurrent use.
type Client struct {
	repo              storage.Repository
	logger            log.Logger
	dataDir           string
	engineType        EngineType
	firecrackerBinary string
	imagesDir         string
	imageRepo         string
	closeFn           func() error
}

// New creates a new SDK client backed by a SQLite database.
//
// The caller must call [Client.Close] when done to release the database
// connection. Typically used with defer:
//
//	client, err := lib.New(ctx, lib.Config{})
//	if err != nil {
//	    return err
//	}
//	defer client.Close()
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
		repo:              repo,
		logger:            cfg.Logger,
		dataDir:           cfg.DataDir,
		engineType:        cfg.Engine,
		firecrackerBinary: cfg.FirecrackerBinary,
		imagesDir:         cfg.ImagesDir,
		imageRepo:         cfg.ImageRepo,
		closeFn:           repo.Close,
	}, nil
}

// Close releases resources held by the client, including the database connection.
// After Close returns, the client must not be used.
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
			DataDir:           c.dataDir,
			FirecrackerBinary: c.firecrackerBinary,
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

// newEngineForCreate creates the engine for sandbox creation.
func (c *Client) newEngineForCreate(engineType EngineType) (sandbox.Engine, error) {
	switch engineType {
	case EngineFirecracker:
		return firecracker.NewEngine(firecracker.EngineConfig{
			DataDir:           c.dataDir,
			FirecrackerBinary: c.firecrackerBinary,
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

// Doctor runs preflight health checks for the configured engine.
//
// For [EngineFirecracker], this checks KVM access, required binaries, network
// configuration, and other prerequisites. For [EngineFake], this returns an
// empty slice (nothing to check).
//
// Returns a slice of [CheckResult] describing each check's outcome.
func (c *Client) Doctor(ctx context.Context) ([]CheckResult, error) {
	if c.engineType == EngineFake || c.engineType == "" {
		return []CheckResult{}, nil
	}

	eng, err := c.newEngine(model.SandboxConfig{
		FirecrackerEngine: &model.FirecrackerEngineConfig{},
	})
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	results := eng.Check(ctx)
	return fromInternalCheckResults(results), nil
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
