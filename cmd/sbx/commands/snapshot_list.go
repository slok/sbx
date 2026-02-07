package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/snapshotlist"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/storage/sqlite"
)

// SnapshotListCommand lists all snapshots.
type SnapshotListCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	format string
}

// NewSnapshotListCommand returns the snapshot list command.
func NewSnapshotListCommand(rootCmd *RootCommand, snapshotCmd *kingpin.CmdClause) *SnapshotListCommand {
	c := &SnapshotListCommand{rootCmd: rootCmd}

	c.Cmd = snapshotCmd.Command("list", "List all snapshots.")
	c.Cmd.Flag("format", "Output format (table, json).").Default("table").EnumVar(&c.format, "table", "json")

	return c
}

func (c SnapshotListCommand) Name() string { return c.Cmd.FullCommand() }

func (c SnapshotListCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	svc, err := snapshotlist.NewService(snapshotlist.ServiceConfig{
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	snapshots, err := svc.Run(ctx, snapshotlist.Request{})
	if err != nil {
		return fmt.Errorf("could not list snapshots: %w", err)
	}

	// Print output.
	var p printer.Printer
	switch c.format {
	case "json":
		p = printer.NewJSONPrinter(c.rootCmd.Stdout)
	default: // table
		p = printer.NewTablePrinter(c.rootCmd.Stdout)
	}

	if err := p.PrintSnapshotList(snapshots); err != nil {
		return fmt.Errorf("could not print snapshot list: %w", err)
	}

	return nil
}
