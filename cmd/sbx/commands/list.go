package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/list"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type ListCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	statusFilter string
	format       string
}

// NewListCommand returns the list command.
func NewListCommand(rootCmd *RootCommand, app *kingpin.Application) *ListCommand {
	c := &ListCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("list", "List all sandboxes.")
	c.Cmd.Flag("status", "Filter by status (running, stopped, failed).").StringVar(&c.statusFilter)
	c.Cmd.Flag("format", "Output format (table, json).").Default("table").EnumVar(&c.format, "table", "json")

	return c
}

func (c ListCommand) Name() string { return c.Cmd.FullCommand() }

func (c ListCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Parse status filter if provided.
	var statusFilter *model.SandboxStatus
	if c.statusFilter != "" {
		status := model.SandboxStatus(strings.ToLower(c.statusFilter))
		// Validate status value.
		switch status {
		case model.SandboxStatusPending, model.SandboxStatusRunning, model.SandboxStatusStopped, model.SandboxStatusFailed:
			statusFilter = &status
		default:
			return fmt.Errorf("invalid status filter: %s (must be: running, stopped, pending, failed)", c.statusFilter)
		}
	}

	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Create list service.
	svc, err := list.NewService(list.ServiceConfig{
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute list.
	sandboxes, err := svc.Run(ctx, list.Request{
		StatusFilter: statusFilter,
	})
	if err != nil {
		return fmt.Errorf("could not list sandboxes: %w", err)
	}

	// Print output.
	var p printer.Printer
	switch c.format {
	case "json":
		p = printer.NewJSONPrinter(c.rootCmd.Stdout)
	default: // table
		p = printer.NewTablePrinter(c.rootCmd.Stdout)
	}

	if err := p.PrintList(sandboxes); err != nil {
		return fmt.Errorf("could not print list: %w", err)
	}

	return nil
}
