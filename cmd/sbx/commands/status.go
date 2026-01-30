package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/status"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type StatusCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
	format   string
}

// NewStatusCommand returns the status command.
func NewStatusCommand(rootCmd *RootCommand, app *kingpin.Application) *StatusCommand {
	c := &StatusCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("status", "Get detailed status of a sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)
	c.Cmd.Flag("format", "Output format (table, json).").Default("table").EnumVar(&c.format, "table", "json")

	return c
}

func (c StatusCommand) Name() string { return c.Cmd.FullCommand() }

func (c StatusCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Create status service.
	svc, err := status.NewService(status.ServiceConfig{
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute status.
	sandbox, err := svc.Run(ctx, status.Request{
		NameOrID: c.nameOrID,
	})
	if err != nil {
		return fmt.Errorf("could not get sandbox status: %w", err)
	}

	// Print output.
	var p printer.Printer
	switch c.format {
	case "json":
		p = printer.NewJSONPrinter(c.rootCmd.Stdout)
	default: // table
		p = printer.NewTablePrinter(c.rootCmd.Stdout)
	}

	if err := p.PrintStatus(*sandbox); err != nil {
		return fmt.Errorf("could not print status: %w", err)
	}

	return nil
}
