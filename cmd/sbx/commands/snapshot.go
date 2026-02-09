package commands

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"
	"k8s.io/client-go/util/homedir"

	"github.com/slok/sbx/internal/app/imagecreate"
	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/storage/sqlite"
)

type SnapshotCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	sandboxNameOrID string
	imageName       string
	imagesDir       string
}

func NewSnapshotCommand(rootCmd *RootCommand, app *kingpin.Application) *SnapshotCommand {
	c := &SnapshotCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("snapshot", "Create a snapshot image from a sandbox.")
	c.Cmd.Arg("sandbox", "Name or ID of the sandbox to snapshot.").Required().StringVar(&c.sandboxNameOrID)
	c.Cmd.Flag("name", "Name for the snapshot image. Auto-generated if not provided.").StringVar(&c.imageName)

	defaultImagesDir := filepath.Join(homedir.HomeDir(), image.DefaultImagesDir)
	c.Cmd.Flag("images-dir", "Local directory for images.").Default(defaultImagesDir).StringVar(&c.imagesDir)

	return c
}

func (c SnapshotCommand) Name() string { return c.Cmd.FullCommand() }

func (c SnapshotCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Initialize storage.
	repo, err := sqlite.NewRepository(ctx, sqlite.RepositoryConfig{
		DBPath: c.rootCmd.DBPath,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("could not create repository: %w", err)
	}

	// Initialize snapshot manager.
	snapMgr, err := image.NewLocalSnapshotManager(image.LocalSnapshotManagerConfig{
		ImagesDir: c.imagesDir,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("could not create snapshot manager: %w", err)
	}

	// Determine data dir from images dir (go up one level: ~/.sbx/images -> ~/.sbx).
	dataDir := filepath.Dir(c.imagesDir)

	svc, err := imagecreate.NewService(imagecreate.ServiceConfig{
		SnapshotManager: snapMgr,
		Repository:      repo,
		Logger:          logger,
		DataDir:         dataDir,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	imgName, err := svc.Run(ctx, imagecreate.Request{
		NameOrID:  c.sandboxNameOrID,
		ImageName: c.imageName,
	})
	if err != nil {
		return fmt.Errorf("could not create snapshot image: %w", err)
	}

	fmt.Fprintf(c.rootCmd.Stdout, "Snapshot image created: %s\n", imgName)
	fmt.Fprintf(c.rootCmd.Stdout, "  Use 'sbx create --from-image %s' to create a sandbox from this image.\n", imgName)
	return nil
}
