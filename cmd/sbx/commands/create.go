package commands

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"
	"k8s.io/client-go/util/homedir"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/image"
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

	// Image flags.
	fromImage string
	imageRepo string
	imagesDir string
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

	// Image flags.
	c.Cmd.Flag("from-image", "Use a pulled image version (e.g. v0.1.0). Run 'sbx image pull' first.").StringVar(&c.fromImage)
	c.Cmd.Flag("image-repo", "GitHub repository for images (used with --from-image).").Default(image.DefaultRepo).StringVar(&c.imageRepo)

	defaultImagesDir := filepath.Join(homedir.HomeDir(), image.DefaultImagesDir)
	c.Cmd.Flag("images-dir", "Local directory for images (used with --from-image).").Default(defaultImagesDir).StringVar(&c.imagesDir)

	return c
}

func (c CreateCommand) Name() string { return c.Cmd.FullCommand() }

func (c CreateCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Validate conflicting flags.
	if c.fromImage != "" && c.firecrackerRootFS != "" {
		return fmt.Errorf("--from-image and --firecracker-root-fs cannot be used together")
	}
	if c.fromImage != "" && c.firecrackerKernel != "" {
		return fmt.Errorf("--from-image and --firecracker-kernel cannot be used together")
	}
	// Initialize storage (SQLite).
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Resolve image paths if --from-image is set.
	var firecrackerBinaryPath string
	if c.fromImage != "" {
		// Try snapshot manager first, then image manager.
		snapMgr, err := image.NewLocalSnapshotManager(image.LocalSnapshotManagerConfig{
			ImagesDir: c.imagesDir,
			Logger:    logger,
		})
		if err != nil {
			return fmt.Errorf("could not create snapshot manager: %w", err)
		}

		exists, err := snapMgr.Exists(ctx, c.fromImage)
		if err == nil && exists {
			c.firecrackerKernel = snapMgr.KernelPath(c.fromImage)
			c.firecrackerRootFS = snapMgr.RootFSPath(c.fromImage)
			firecrackerBinaryPath = snapMgr.FirecrackerPath(c.fromImage)
		} else {
			// Fall back to image manager (GitHub releases).
			mgr, err := image.NewGitHubImageManager(image.GitHubImageManagerConfig{
				Repo:       c.imageRepo,
				ImagesDir:  c.imagesDir,
				HTTPClient: http.DefaultClient,
				Logger:     logger,
			})
			if err != nil {
				return fmt.Errorf("could not create image manager: %w", err)
			}

			exists, err := mgr.Exists(ctx, c.fromImage)
			if err != nil {
				return fmt.Errorf("could not check image: %w", err)
			}
			if !exists {
				return fmt.Errorf("image %s is not installed, run 'sbx image pull %s' first", c.fromImage, c.fromImage)
			}

			c.firecrackerKernel = mgr.KernelPath(c.fromImage)
			c.firecrackerRootFS = mgr.RootFSPath(c.fromImage)
			firecrackerBinaryPath = mgr.FirecrackerPath(c.fromImage)
		}
	}

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
			return fmt.Errorf("--firecracker-root-fs or --from-image is required when using firecracker engine")
		}
		if c.firecrackerKernel == "" {
			return fmt.Errorf("--firecracker-kernel or --from-image is required when using firecracker engine")
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

	// Initialize engine based on config.
	var eng sandbox.Engine
	switch c.engine {
	case "firecracker":
		eng, err = firecracker.NewEngine(firecracker.EngineConfig{
			FirecrackerBinary: firecrackerBinaryPath,
			Repository:        repo,
			Logger:            logger,
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
