package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/engine/fake"
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

	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Initialize engine (fake for now).
	eng, err := fake.NewEngine(fake.EngineConfig{
		Logger: logger,
	})
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
		ConfigPath:   c.configFile,
		NameOverride: c.nameOverride,
	})
	if err != nil {
		return fmt.Errorf("could not create sandbox: %w", err)
	}

	// Output success message.
	fmt.Fprintf(c.rootCmd.Stdout, "Sandbox created successfully!\n")
	fmt.Fprintf(c.rootCmd.Stdout, "  ID:     %s\n", sandbox.ID)
	fmt.Fprintf(c.rootCmd.Stdout, "  Name:   %s\n", sandbox.Name)
	fmt.Fprintf(c.rootCmd.Stdout, "  Status: %s\n", sandbox.Status)
	fmt.Fprintf(c.rootCmd.Stdout, "  Base:   %s\n", sandbox.Config.Base)

	return nil
}
