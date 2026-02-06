package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/sandbox/firecracker"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type CreateCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	// Required flags.
	name   string
	engine string

	// Resource flags.
	cpu  float64
	mem  int
	disk int

	// Firecracker-specific flags.
	firecrackerRootFS string
	firecrackerKernel string
}

// NewCreateCommand returns the create command.
func NewCreateCommand(rootCmd *RootCommand, app *kingpin.Application) *CreateCommand {
	c := &CreateCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("create", "Create a new sandbox.")

	// Required flags.
	c.Cmd.Flag("name", "Name for the sandbox.").Short('n').Required().StringVar(&c.name)
	c.Cmd.Flag("engine", "Engine type (firecracker, fake).").Required().EnumVar(&c.engine, "firecracker", "fake")

	// Resource flags.
	c.Cmd.Flag("cpu", "Number of VCPUs (can be fractional, e.g., 0.5, 1.5).").Default("2").Float64Var(&c.cpu)
	c.Cmd.Flag("mem", "Memory in MB.").Default("2048").IntVar(&c.mem)
	c.Cmd.Flag("disk", "Disk in GB.").Default("10").IntVar(&c.disk)

	// Firecracker-specific flags.
	c.Cmd.Flag("firecracker-root-fs", "Path to rootfs image (required for firecracker engine).").StringVar(&c.firecrackerRootFS)
	c.Cmd.Flag("firecracker-kernel", "Path to kernel image (required for firecracker engine).").StringVar(&c.firecrackerKernel)

	return c
}

func (c CreateCommand) Name() string { return c.Cmd.FullCommand() }

func (c CreateCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Build SandboxConfig from CLI flags.
	cfg := model.SandboxConfig{
		Name: c.name,
		Resources: model.Resources{
			VCPUs:    c.cpu,
			MemoryMB: c.mem,
			DiskGB:   c.disk,
		},
	}

	switch c.engine {
	case "firecracker":
		if c.firecrackerRootFS == "" {
			return fmt.Errorf("--firecracker-root-fs is required when using firecracker engine")
		}
		if c.firecrackerKernel == "" {
			return fmt.Errorf("--firecracker-kernel is required when using firecracker engine")
		}
		cfg.FirecrackerEngine = &model.FirecrackerEngineConfig{
			RootFS:      c.firecrackerRootFS,
			KernelImage: c.firecrackerKernel,
		}
	case "fake":
		cfg.FirecrackerEngine = &model.FirecrackerEngineConfig{
			RootFS:      "/fake/rootfs.ext4",
			KernelImage: "/fake/vmlinux",
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

	// Initialize engine based on config.
	var eng sandbox.Engine
	switch c.engine {
	case "firecracker":
		eng, err = firecracker.NewEngine(firecracker.EngineConfig{
			Repository: repo,
			Logger:     logger,
		})
	case "fake":
		eng, err = fake.NewEngine(fake.EngineConfig{
			Logger: logger,
		})
	}
	if err != nil {
		return fmt.Errorf("could not create engine: %w", err)
	}

	// Create service.
	svc, err := create.NewService(create.ServiceConfig{
		Engine:     eng,
		Repository: repo,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	// Execute create.
	sb, err := svc.Create(ctx, create.CreateOptions{
		Config: cfg,
	})
	if err != nil {
		return fmt.Errorf("could not create sandbox: %w", err)
	}

	// Output success message.
	fmt.Fprintf(c.rootCmd.Stdout, "Sandbox created successfully!\n")
	fmt.Fprintf(c.rootCmd.Stdout, "  ID:     %s\n", sb.ID)
	fmt.Fprintf(c.rootCmd.Stdout, "  Name:   %s\n", sb.Name)
	fmt.Fprintf(c.rootCmd.Stdout, "  Status: %s\n", sb.Status)
	if sb.Config.FirecrackerEngine != nil {
		fmt.Fprintf(c.rootCmd.Stdout, "  Engine: firecracker\n")
	}

	return nil
}
