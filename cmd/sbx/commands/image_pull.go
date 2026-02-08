package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/imagepull"
	"github.com/slok/sbx/internal/printer"
)

// ImagePullCommand pulls an image release.
type ImagePullCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand
	imgCmd  *ImageCommand

	version string
	force   bool
}

// NewImagePullCommand returns the image pull command.
func NewImagePullCommand(rootCmd *RootCommand, imgCmd *ImageCommand) *ImagePullCommand {
	c := &ImagePullCommand{rootCmd: rootCmd, imgCmd: imgCmd}

	c.Cmd = imgCmd.Cmd.Command("pull", "Pull an image release.")
	c.Cmd.Arg("version", "Image version to pull (e.g. v0.1.0).").Required().StringVar(&c.version)
	c.Cmd.Flag("force", "Force re-download even if already installed.").BoolVar(&c.force)

	return c
}

func (c ImagePullCommand) Name() string { return c.Cmd.FullCommand() }

func (c ImagePullCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	mgr, err := newImageManager(c.imgCmd, logger)
	if err != nil {
		return err
	}

	svc, err := imagepull.NewService(imagepull.ServiceConfig{
		Manager: mgr,
		Logger:  logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, imagepull.Request{
		Version:      c.version,
		Force:        c.force,
		StatusWriter: c.rootCmd.Stderr,
	})
	if err != nil {
		return fmt.Errorf("could not pull image: %w", err)
	}

	// Print success message.
	p := printer.NewTablePrinter(c.rootCmd.Stdout)
	if result.Skipped {
		return p.PrintMessage(fmt.Sprintf("Image %s already installed (use --force to re-download)", result.Version))
	}
	return p.PrintMessage(fmt.Sprintf("Successfully pulled image %s", result.Version))
}
