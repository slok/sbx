package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/exec"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/sqlite"
	utilsenv "github.com/slok/sbx/internal/utils/env"
)

type ExecCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID   string
	command    []string
	workingDir string
	envSpecs   []string
	tty        bool
	files      []string
}

// NewExecCommand returns the exec command.
func NewExecCommand(rootCmd *RootCommand, app *kingpin.Application) *ExecCommand {
	c := &ExecCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("exec", "Execute a command in a running sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)
	c.Cmd.Arg("command", "Command to execute (use -- before command).").Required().StringsVar(&c.command)
	c.Cmd.Flag("workdir", "Working directory for command execution.").Short('w').StringVar(&c.workingDir)
	c.Cmd.Flag("env", "Environment variables (KEY=VALUE or KEY from current environment). Can be repeated.").Short('e').StringsVar(&c.envSpecs)
	c.Cmd.Flag("tty", "Allocate a pseudo-TTY.").Short('t').BoolVar(&c.tty)
	c.Cmd.Flag("file", "Upload local file to sandbox before exec (into workdir). Can be repeated.").Short('f').StringsVar(&c.files)

	return c
}

func (c ExecCommand) Name() string { return c.Cmd.FullCommand() }

func (c ExecCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	cmdEnv, err := utilsenv.ParseSpecs(c.envSpecs)
	if err != nil {
		return fmt.Errorf("invalid --env value: %w", err)
	}

	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
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
	eng, err := newEngineFromConfig(sandbox.Config, repo, logger)
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

	// Execute command with stdin/stdout/stderr wired directly to the terminal.
	result, err := svc.Run(ctx, exec.Request{
		NameOrID: c.nameOrID,
		Command:  c.command,
		Files:    c.files,
		Opts: model.ExecOpts{
			WorkingDir: c.workingDir,
			Env:        cmdEnv,
			Stdin:      os.Stdin,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
			Tty:        c.tty,
		},
	})
	if err != nil {
		return fmt.Errorf("could not execute command: %w", err)
	}

	// Exit with the command's exit code
	os.Exit(result.ExitCode)
	return nil
}
