package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/forward"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type ForwardCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	nameOrID string
	ports    []string
}

// NewForwardCommand returns the forward command.
func NewForwardCommand(rootCmd *RootCommand, app *kingpin.Application) *ForwardCommand {
	c := &ForwardCommand{
		rootCmd: rootCmd,
	}

	c.Cmd = app.Command("forward", "Forward ports from localhost to a running sandbox.")
	c.Cmd.Arg("name-or-id", "Sandbox name or ID.").Required().StringVar(&c.nameOrID)
	c.Cmd.Arg("ports", "Port mappings (e.g., 8080 or 8080:8080).").Required().StringsVar(&c.ports)

	return c
}

func (c ForwardCommand) Name() string { return c.Cmd.FullCommand() }

func (c ForwardCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Parse port mappings
	portMappings := make([]model.PortMapping, 0, len(c.ports))
	for _, p := range c.ports {
		pm, err := model.ParsePortMapping(p)
		if err != nil {
			return fmt.Errorf("invalid port mapping %q: %w", p, err)
		}
		portMappings = append(portMappings, pm)
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
	sandbox, err := repo.GetSandboxByName(ctx, c.nameOrID)
	if err != nil {
		// Try by ID if name lookup failed
		sandbox, err = repo.GetSandbox(ctx, c.nameOrID)
		if err != nil {
			return fmt.Errorf("could not find sandbox: %w", err)
		}
	}

	// Initialize engine based on sandbox configuration.
	eng, err := newEngineFromConfig(sandbox.Config, repo, taskRepo, logger)
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	// Create forward service.
	svc, err := forward.NewService(forward.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Print forwarding info
	fmt.Fprintf(c.rootCmd.Stdout, "Forwarding ports for %s:\n", sandbox.Name)
	for _, pm := range portMappings {
		fmt.Fprintf(c.rootCmd.Stdout, "  localhost:%d -> sandbox:%d\n", pm.LocalPort, pm.RemotePort)
	}
	fmt.Fprintln(c.rootCmd.Stdout)
	fmt.Fprintln(c.rootCmd.Stdout, "Press Ctrl+C to stop")

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintln(c.rootCmd.Stdout) // New line after ^C
		cancel()
	}()

	// Start port forwarding (blocks until cancelled)
	if err := svc.Run(ctx, forward.Request{
		NameOrID: c.nameOrID,
		Ports:    portMappings,
	}); err != nil {
		return fmt.Errorf("port forwarding failed: %w", err)
	}

	return nil
}
