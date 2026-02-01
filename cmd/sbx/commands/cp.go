package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/copy"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type CpCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	source      string
	destination string
}

// NewCpCommand returns the cp command.
func NewCpCommand(rootCmd *RootCommand, app *kingpin.Application) *CpCommand {
	c := &CpCommand{
		rootCmd: rootCmd,
	}

	c.Cmd = app.Command("cp", "Copy files between host and sandbox.")
	c.Cmd.Arg("source", "Source path (local path or sandbox:/path).").Required().StringVar(&c.source)
	c.Cmd.Arg("destination", "Destination path (local path or sandbox:/path).").Required().StringVar(&c.destination)

	return c
}

func (c CpCommand) Name() string { return c.Cmd.FullCommand() }

func (c CpCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Parse arguments to determine sandbox reference.
	parsed, err := copy.ParseCopyArgs(c.source, c.destination)
	if err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

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
	sandbox, err := repo.GetSandboxByName(ctx, parsed.SandboxRef)
	if err != nil {
		// Try by ID if name lookup failed
		sandbox, err = repo.GetSandbox(ctx, parsed.SandboxRef)
		if err != nil {
			return fmt.Errorf("could not find sandbox '%s': %w", parsed.SandboxRef, err)
		}
	}

	// Initialize engine based on sandbox configuration.
	eng, err := newEngineFromConfig(sandbox.Config, repo, taskRepo, logger)
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	// Create copy service.
	svc, err := copy.NewService(copy.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute copy operation.
	if err := svc.Run(ctx, copy.Request{
		Source:      c.source,
		Destination: c.destination,
	}); err != nil {
		return err
	}

	// Print success message.
	if parsed.ToSandbox {
		fmt.Fprintf(c.rootCmd.Stdout, "Copied %s to %s:%s\n", parsed.LocalPath, sandbox.Name, parsed.RemotePath)
	} else {
		fmt.Fprintf(c.rootCmd.Stdout, "Copied %s:%s to %s\n", sandbox.Name, parsed.RemotePath, parsed.LocalPath)
	}

	return nil
}

// parsePathForSandbox extracts sandbox reference from a path with colon syntax.
// Returns empty string if path doesn't have sandbox reference.
func parsePathForSandbox(path string) string {
	if !strings.Contains(path, ":") {
		return ""
	}
	parts := strings.SplitN(path, ":", 2)
	return parts[0]
}
