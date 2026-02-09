package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/imagerm"
	"github.com/slok/sbx/internal/printer"
)

// ImageRmCommand removes an installed image.
type ImageRmCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand
	imgCmd  *ImageCommand

	version string
}

// NewImageRmCommand returns the image rm command.
func NewImageRmCommand(rootCmd *RootCommand, imgCmd *ImageCommand) *ImageRmCommand {
	c := &ImageRmCommand{rootCmd: rootCmd, imgCmd: imgCmd}

	c.Cmd = imgCmd.Cmd.Command("rm", "Remove an installed image.")
	c.Cmd.Arg("version", "Image version to remove (e.g. v0.1.0).").Required().StringVar(&c.version)

	return c
}

func (c ImageRmCommand) Name() string { return c.Cmd.FullCommand() }

func (c ImageRmCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	mgr, err := newLocalImageManager(c.imgCmd, logger)
	if err != nil {
		return err
	}

	svc, err := imagerm.NewService(imagerm.ServiceConfig{
		Manager: mgr,
		Logger:  logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	if err := svc.Run(ctx, imagerm.Request{Version: c.version}); err != nil {
		return fmt.Errorf("could not remove image: %w", err)
	}

	p := printer.NewTablePrinter(c.rootCmd.Stdout)
	return p.PrintMessage(fmt.Sprintf("Removed image %s", c.version))
}
