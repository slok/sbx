package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/imageinspect"
	"github.com/slok/sbx/internal/printer"
)

// ImageInspectCommand inspects an image release manifest.
type ImageInspectCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand
	imgCmd  *ImageCommand

	version string
	format  string
}

// NewImageInspectCommand returns the image inspect command.
func NewImageInspectCommand(rootCmd *RootCommand, imgCmd *ImageCommand) *ImageInspectCommand {
	c := &ImageInspectCommand{rootCmd: rootCmd, imgCmd: imgCmd}

	c.Cmd = imgCmd.Cmd.Command("inspect", "Inspect an image release manifest.")
	c.Cmd.Arg("version", "Image version to inspect (e.g. v0.1.0).").Required().StringVar(&c.version)
	c.Cmd.Flag("format", "Output format (table, json).").Default("table").EnumVar(&c.format, "table", "json")

	return c
}

func (c ImageInspectCommand) Name() string { return c.Cmd.FullCommand() }

func (c ImageInspectCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	mgr, err := newImageManager(c.imgCmd, logger)
	if err != nil {
		return err
	}

	snapMgr, err := newSnapshotManager(c.imgCmd, logger)
	if err != nil {
		return err
	}

	svc, err := imageinspect.NewService(imageinspect.ServiceConfig{
		Manager:         mgr,
		SnapshotManager: snapMgr,
		Logger:          logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	manifest, err := svc.Run(ctx, imageinspect.Request{Version: c.version})
	if err != nil {
		return fmt.Errorf("could not inspect image: %w", err)
	}

	// Print output.
	var p printer.Printer
	switch c.format {
	case "json":
		p = printer.NewJSONPrinter(c.rootCmd.Stdout)
	default:
		p = printer.NewTablePrinter(c.rootCmd.Stdout)
	}

	if err := p.PrintImageInspect(*manifest); err != nil {
		return fmt.Errorf("could not print image manifest: %w", err)
	}

	return nil
}
