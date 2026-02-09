package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/app/imagelist"
	"github.com/slok/sbx/internal/printer"
)

// ImageListCommand lists available image releases.
type ImageListCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand
	imgCmd  *ImageCommand

	format string
}

// NewImageListCommand returns the image list command.
func NewImageListCommand(rootCmd *RootCommand, imgCmd *ImageCommand) *ImageListCommand {
	c := &ImageListCommand{rootCmd: rootCmd, imgCmd: imgCmd}

	c.Cmd = imgCmd.Cmd.Command("list", "List available image releases.")
	c.Cmd.Flag("format", "Output format (table, json).").Default("table").EnumVar(&c.format, "table", "json")

	return c
}

func (c ImageListCommand) Name() string { return c.Cmd.FullCommand() }

func (c ImageListCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	mgr, err := newLocalImageManager(c.imgCmd, logger)
	if err != nil {
		return err
	}

	puller, err := newImagePuller(c.imgCmd, logger)
	if err != nil {
		return err
	}

	svc, err := imagelist.NewService(imagelist.ServiceConfig{
		Manager: mgr,
		Puller:  puller,
		Logger:  logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	releases, err := svc.Run(ctx)
	if err != nil {
		return fmt.Errorf("could not list image releases: %w", err)
	}

	// Print output.
	var p printer.Printer
	switch c.format {
	case "json":
		p = printer.NewJSONPrinter(c.rootCmd.Stdout)
	default:
		p = printer.NewTablePrinter(c.rootCmd.Stdout)
	}

	if err := p.PrintImageList(releases); err != nil {
		return fmt.Errorf("could not print image list: %w", err)
	}

	return nil
}
