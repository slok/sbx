package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/start"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type StartCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
}

// NewStartCommand returns the start command.
func NewStartCommand(rootCmd *RootCommand, app *kingpin.Application) *StartCommand {
	c := &StartCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("start", "Start a stopped sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)

	return c
}

func (c StartCommand) Name() string { return c.Cmd.FullCommand() }

func (c StartCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Initialize task manager with the same database connection.
	taskRepo, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{
		DB:     repo.DB(),
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create task manager: %w", err)
	}

	// Get sandbox to determine which engine to use.
	sandbox, err := repo.GetSandboxByName(ctx, c.nameOrID)
	if err != nil {
		// Try by ID if name lookup failed
		sandbox, err = repo.GetSandbox(ctx, c.nameOrID)
		if err != nil {
			return fmt.Errorf("could not find sandbox: %w", err)
		}
	}

	// Initialize engine based on sandbox configuration.
	eng, err := newEngineFromConfig(sandbox.Config, repo, taskRepo, logger)
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	// Create start service.
	svc, err := start.NewService(start.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute start.
	sandbox, err = svc.Run(ctx, start.Request{
		NameOrID: c.nameOrID,
	})
	if err != nil {
		return fmt.Errorf("could not start sandbox: %w", err)
	}

	// Print success message.
	p := printer.NewTablePrinter(c.rootCmd.Stdout)
	if err := p.PrintMessage(fmt.Sprintf("Started sandbox: %s", sandbox.Name)); err != nil {
		return fmt.Errorf("could not print message: %w", err)
	}

	return nil
}
