package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/exec"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type ShellCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
}

// NewShellCommand returns the shell command.
func NewShellCommand(rootCmd *RootCommand, app *kingpin.Application) *ShellCommand {
	c := &ShellCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("shell", "Open an interactive shell in a running sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)

	return c
}

func (c ShellCommand) Name() string { return c.Cmd.FullCommand() }

func (c ShellCommand) Run(ctx context.Context) error {
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
	eng, err := newEngineFromConfig(sandbox.Config, taskRepo, logger)
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	// Create exec service.
	svc, err := exec.NewService(exec.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute /bin/sh with TTY for interactive shell.
	result, err := svc.Run(ctx, exec.Request{
		NameOrID: c.nameOrID,
		Command:  []string{"/bin/sh"},
		Opts: model.ExecOpts{
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Tty:    true,
		},
	})
	if err != nil {
		return fmt.Errorf("could not open shell: %w", err)
	}

	// Exit with the shell's exit code
	os.Exit(result.ExitCode)
	return nil
}
