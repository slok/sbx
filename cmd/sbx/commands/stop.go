package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/stop"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type StopCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
}

// NewStopCommand returns the stop command.
func NewStopCommand(rootCmd *RootCommand, app *kingpin.Application) *StopCommand {
	c := &StopCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("stop", "Stop a running sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)

	return c
}

func (c StopCommand) Name() string { return c.Cmd.FullCommand() }

func (c StopCommand) Run(ctx context.Context) error {
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

	// Create stop service.
	svc, err := stop.NewService(stop.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute stop.
	sandbox, err := svc.Run(ctx, stop.Request{
		NameOrID: c.nameOrID,
	})
	if err != nil {
		return fmt.Errorf("could not stop sandbox: %w", err)
	}

	// Print success message.
	p := printer.NewTablePrinter(c.rootCmd.Stdout)
	if err := p.PrintMessage(fmt.Sprintf("Stopped sandbox: %s", sandbox.Name)); err != nil {
		return fmt.Errorf("could not print message: %w", err)
	}

	return nil
}
