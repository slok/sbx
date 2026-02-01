package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/sandbox/docker"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/sandbox/firecracker"
	"github.com/slok/sbx/internal/storage/io"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type CreateCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	configFile   string
	nameOverride string
}

// NewCreateCommand returns the create command.
func NewCreateCommand(rootCmd *RootCommand, app *kingpin.Application) *CreateCommand {
	c := &CreateCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("create", "Create a new sandbox.")
	c.Cmd.Flag("file", "Path to the sandbox configuration YAML file.").Short('f').Required().StringVar(&c.configFile)
	c.Cmd.Flag("name", "Override the sandbox name from the config file.").Short('n').StringVar(&c.nameOverride)

	return c
}

func (c CreateCommand) Name() string { return c.Cmd.FullCommand() }

func (c CreateCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Convert config file path to absolute path.
	configPath := c.configFile
	if !filepath.IsAbs(configPath) {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			return fmt.Errorf("could not resolve config path: %w", err)
		}
		configPath = absPath
	}

	// Load configuration from YAML file using ConfigYAMLRepository.
	// We use absolute paths so DirFS("/") works correctly.
	configRepo := io.NewConfigYAMLRepository(os.DirFS("/"))
	// Remove leading "/" for fs.FS which expects relative paths.
	cfg, err := configRepo.GetConfig(ctx, configPath[1:])
	if err != nil {
		return fmt.Errorf("could not load config: %w", err)
	}

	// Apply name override if provided.
	if c.nameOverride != "" {
		cfg.Name = c.nameOverride
	}

	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Initialize task repository with the same database connection.
	taskRepo, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{
		DB:     repo.DB(),
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create task repository: %w", err)
	}

	// Initialize engine based on config.
	var eng sandbox.Engine
	if cfg.DockerEngine != nil {
		eng, err = docker.NewEngine(docker.EngineConfig{
			TaskRepo: taskRepo,
			Logger:   logger,
		})
	} else if cfg.FirecrackerEngine != nil {
		eng, err = firecracker.NewEngine(firecracker.EngineConfig{
			TaskRepo: taskRepo,
			Logger:   logger,
		})
	} else {
		// Fallback to fake engine for testing
		eng, err = fake.NewEngine(fake.EngineConfig{
			TaskRepo: taskRepo,
			Logger:   logger,
		})
	}
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	// Create service.
	svc, err := create.NewService(create.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute create.
	sandbox, err := svc.Create(ctx, create.CreateOptions{
		Config: cfg,
	})
	if err != nil {
		return fmt.Errorf("could not create sandbox: %w", err)
	}

	// Output success message.
	fmt.Fprintf(c.rootCmd.Stdout, "Sandbox created successfully!\n")
	fmt.Fprintf(c.rootCmd.Stdout, "  ID:     %s\n", sandbox.ID)
	fmt.Fprintf(c.rootCmd.Stdout, "  Name:   %s\n", sandbox.Name)
	fmt.Fprintf(c.rootCmd.Stdout, "  Status: %s\n", sandbox.Status)
	if sandbox.Config.DockerEngine != nil {
		fmt.Fprintf(c.rootCmd.Stdout, "  Engine: docker\n")
		fmt.Fprintf(c.rootCmd.Stdout, "  Image:  %s\n", sandbox.Config.DockerEngine.Image)
	} else if sandbox.Config.FirecrackerEngine != nil {
		fmt.Fprintf(c.rootCmd.Stdout, "  Engine: firecracker\n")
	}

	return nil
}
