package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/remove"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type RemoveCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
	force    bool
}

// NewRemoveCommand returns the remove command.
func NewRemoveCommand(rootCmd *RootCommand, app *kingpin.Application) *RemoveCommand {
	c := &RemoveCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("rm", "Remove a sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)
	c.Cmd.Flag("force", "Force removal of a running sandbox.").BoolVar(&c.force)

	return c
}

func (c RemoveCommand) Name() string { return c.Cmd.FullCommand() }

func (c RemoveCommand) Run(ctx context.Context) error {
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

	// Create remove service.
	svc, err := remove.NewService(remove.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute remove.
	sandbox, err = svc.Run(ctx, remove.Request{
		NameOrID: c.nameOrID,
		Force:    c.force,
	})
	if err != nil {
		return fmt.Errorf("could not remove sandbox: %w", err)
	}

	// Print success message.
	p := printer.NewTablePrinter(c.rootCmd.Stdout)
	msg := fmt.Sprintf("Removed sandbox: %s", sandbox.Name)
	if c.force && sandbox.Status == "running" {
		msg = fmt.Sprintf("Stopped and removed sandbox: %s", sandbox.Name)
	}
	if err := p.PrintMessage(msg); err != nil {
		return fmt.Errorf("could not print message: %w", err)
	}

	return nil
}
