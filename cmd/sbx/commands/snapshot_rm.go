package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/snapshotremove"
	"github.com/slok/sbx/internal/printer"
	"github.com/slok/sbx/internal/storage/sqlite"
)

// SnapshotRmCommand removes a snapshot.
type SnapshotRmCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
}

// NewSnapshotRmCommand returns the snapshot rm command.
func NewSnapshotRmCommand(rootCmd *RootCommand, snapshotCmd *kingpin.CmdClause) *SnapshotRmCommand {
	c := &SnapshotRmCommand{rootCmd: rootCmd}

	c.Cmd = snapshotCmd.Command("rm", "Remove a snapshot.")
	c.Cmd.Arg("name-or-id", "Snapshot name or ID.").Required().StringVar(&c.nameOrID)

	return c
}

func (c SnapshotRmCommand) Name() string { return c.Cmd.FullCommand() }

func (c SnapshotRmCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	svc, err := snapshotremove.NewService(snapshotremove.ServiceConfig{
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	snapshot, err := svc.Run(ctx, snapshotremove.Request{
		NameOrID: c.nameOrID,
	})
	if err != nil {
		return fmt.Errorf("could not remove snapshot: %w", err)
	}

	p := printer.NewTablePrinter(c.rootCmd.Stdout)
	if err := p.PrintMessage(fmt.Sprintf("Removed snapshot: %s", snapshot.Name)); err != nil {
		return fmt.Errorf("could not print message: %w", err)
	}

	return nil
}
