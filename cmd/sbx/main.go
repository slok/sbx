package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	"github.com/oklog/run"
	"github.com/sirupsen/logrus"

	"github.com/slok/sbx/cmd/sbx/commands"
	"github.com/slok/sbx/internal/log"
	loglogrus "github.com/slok/sbx/internal/log/logrus"
)

const (
	// Version is the application version (set via ldflags).
	Version = "dev"
)

// Run runs the main application.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	app := kingpin.New("sbx", "MicroVM sandbox management tool.")
	app.DefaultEnvars()
	rootCmd := commands.NewRootCommand(app)

	// Setup commands (registers flags).
	createCmd := commands.NewCreateCommand(rootCmd, app)
	listCmd := commands.NewListCommand(rootCmd, app)
	statusCmd := commands.NewStatusCommand(rootCmd, app)
	stopCmd := commands.NewStopCommand(rootCmd, app)
	startCmd := commands.NewStartCommand(rootCmd, app)
	removeCmd := commands.NewRemoveCommand(rootCmd, app)
	execCmd := commands.NewExecCommand(rootCmd, app)
	shellCmd := commands.NewShellCommand(rootCmd, app)
	doctorCmd := commands.NewDoctorCommand(rootCmd, app)
	cpCmd := commands.NewCpCommand(rootCmd, app)
	forwardCmd := commands.NewForwardCommand(rootCmd, app)

	// Snapshot subcommands share a parent command.
	snapshotCmd := app.Command("snapshot", "Manage snapshots.")
	snapshotCreateCmd := commands.NewSnapshotCreateCommand(rootCmd, snapshotCmd)
	snapshotListCmd := commands.NewSnapshotListCommand(rootCmd, snapshotCmd)
	snapshotRmCmd := commands.NewSnapshotRmCommand(rootCmd, snapshotCmd)

	// Image subcommands share a parent command.
	imgCmd := commands.NewImageCommand(app)
	imageListCmd := commands.NewImageListCommand(rootCmd, imgCmd)
	imagePullCmd := commands.NewImagePullCommand(rootCmd, imgCmd)
	imageRmCmd := commands.NewImageRmCommand(rootCmd, imgCmd)
	imageInspectCmd := commands.NewImageInspectCommand(rootCmd, imgCmd)

	cmds := map[string]commands.Command{
		createCmd.Name():         createCmd,
		listCmd.Name():           listCmd,
		statusCmd.Name():         statusCmd,
		stopCmd.Name():           stopCmd,
		startCmd.Name():          startCmd,
		removeCmd.Name():         removeCmd,
		execCmd.Name():           execCmd,
		shellCmd.Name():          shellCmd,
		doctorCmd.Name():         doctorCmd,
		cpCmd.Name():             cpCmd,
		forwardCmd.Name():        forwardCmd,
		snapshotCreateCmd.Name(): snapshotCreateCmd,
		snapshotListCmd.Name():   snapshotListCmd,
		snapshotRmCmd.Name():     snapshotRmCmd,
		imageListCmd.Name():      imageListCmd,
		imagePullCmd.Name():      imagePullCmd,
		imageRmCmd.Name():        imageRmCmd,
		imageInspectCmd.Name():   imageInspectCmd,
	}

	// Parse command.
	cmdName, err := app.Parse(args[1:])
	if err != nil {
		return fmt.Errorf("invalid command configuration: %w", err)
	}

	// Set standard input/output.
	rootCmd.Stdin = stdin
	rootCmd.Stdout = stdout
	rootCmd.Stderr = stderr

	// Auto-suppress logging for commands that produce structured output (table/JSON)
	// to prevent log noise from mixing with printer output in the terminal.
	// Users can still enable logging with --debug.
	printerCommands := map[string]bool{
		"list":          true,
		"status":        true,
		"image list":    true,
		"image inspect": true,
		"snapshot list": true,
	}
	if printerCommands[cmdName] && !rootCmd.Debug {
		rootCmd.NoLog = true
	}

	// Set logger.
	rootCmd.Logger = getLogger(ctx, *rootCmd)

	var g run.Group

	// OS signals.
	{
		signalCtx, signalCancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer signalCancel()

		g.Add(
			func() error {
				<-signalCtx.Done()
				rootCmd.Logger.Debugf("Termination signal received")
				return nil
			},
			func(_ error) {
				signalCancel()
			},
		)
	}

	// Execute command.
	{
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		g.Add(
			func() error {
				err := cmds[cmdName].Run(ctx)
				if err != nil {
					return fmt.Errorf("%q command failed: %w", cmdName, err)
				}
				return nil
			},
			func(_ error) {
				cancel()
			},
		)
	}

	return g.Run()
}

// getLogger returns the application logger.
func getLogger(ctx context.Context, config commands.RootCommand) log.Logger {
	if config.NoLog {
		return log.Noop
	}

	// If logger not disabled use logrus logger.
	logrusLog := logrus.New()
	logrusLog.Out = config.Stderr // By default logger goes to stderr (so it can split stdout prints).
	logrusLogEntry := logrus.NewEntry(logrusLog)

	if config.Debug {
		logrusLogEntry.Logger.SetLevel(logrus.DebugLevel)
	}

	// Log format.
	switch config.LoggerType {
	case commands.LoggerTypeDefault:
		logrusLogEntry.Logger.SetFormatter(&logrus.TextFormatter{
			ForceColors:   !config.NoColor,
			DisableColors: config.NoColor,
		})
	case commands.LoggerTypeJSON:
		logrusLogEntry.Logger.SetFormatter(&logrus.JSONFormatter{})
	}

	logger := loglogrus.NewLogrus(logrusLogEntry).WithValues(log.Kv{
		"version": Version,
	})

	logger.Debugf("Debug level is enabled") // Will log only when debug enabled.

	return logger
}

func main() {
	ctx := context.Background()
	err := Run(ctx, os.Args, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
