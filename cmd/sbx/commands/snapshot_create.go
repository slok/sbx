package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/snapshotcreate"
	"github.com/slok/sbx/internal/storage/sqlite"
)

// SnapshotCreateCommand creates snapshots from existing sandboxes.
type SnapshotCreateCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID     string
	snapshotName string
}

// NewSnapshotCreateCommand returns the snapshot create command.
func NewSnapshotCreateCommand(rootCmd *RootCommand, snapshotCmd *kingpin.CmdClause) *SnapshotCreateCommand {
	c := &SnapshotCreateCommand{rootCmd: rootCmd}

	c.Cmd = snapshotCmd.Command("create", "Create a rootfs snapshot from a sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)
	c.Cmd.Arg("snapshot-name", "Optional friendly snapshot name ([a-zA-Z0-9._-]).").StringVar(&c.snapshotName)

	return c
}

func (c SnapshotCreateCommand) Name() string { return c.Cmd.FullCommand() }

func (c SnapshotCreateCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	sandbox, err := repo.GetSandboxByName(ctx, c.nameOrID)
	if err != nil {
		sandbox, err = repo.GetSandbox(ctx, c.nameOrID)
		if err != nil {
			return fmt.Errorf("could not find sandbox: %w", err)
		}
	}

	eng, err := newEngineFromConfig(sandbox.Config, repo, logger)
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	svc, err := snapshotcreate.NewService(snapshotcreate.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	snapshot, err := svc.Run(ctx, snapshotcreate.Request{
		NameOrID:     c.nameOrID,
		SnapshotName: c.snapshotName,
	})
	if err != nil {
		return fmt.Errorf("could not create snapshot: %w", err)
	}

	fmt.Fprintf(c.rootCmd.Stdout, "Snapshot created successfully!\n")
	fmt.Fprintf(c.rootCmd.Stdout, "  ID:                 %s\n", snapshot.ID)
	fmt.Fprintf(c.rootCmd.Stdout, "  Name:               %s\n", snapshot.Name)
	fmt.Fprintf(c.rootCmd.Stdout, "  Source sandbox:     %s (%s)\n", snapshot.SourceSandboxName, snapshot.SourceSandboxID)
	fmt.Fprintf(c.rootCmd.Stdout, "  Path:               %s\n", snapshot.Path)
	fmt.Fprintf(c.rootCmd.Stdout, "  Virtual size:       %d bytes\n", snapshot.VirtualSizeBytes)
	fmt.Fprintf(c.rootCmd.Stdout, "  Allocated on disk:  %d bytes\n", snapshot.AllocatedSizeBytes)

	return nil
}
