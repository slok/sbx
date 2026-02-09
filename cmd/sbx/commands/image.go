package commands

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"
	"k8s.io/client-go/util/homedir"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
)

// ImageCommand is the parent command for image management subcommands.
type ImageCommand struct {
	Cmd *kingpin.CmdClause

	repo      string
	imagesDir string
}

// NewImageCommand returns the image parent command.
func NewImageCommand(app *kingpin.Application) *ImageCommand {
	c := &ImageCommand{}

	c.Cmd = app.Command("image", "Manage VM images.")
	c.Cmd.Flag("repo", "GitHub repository for images.").Default(image.DefaultRepo).StringVar(&c.repo)

	defaultImagesDir := filepath.Join(homedir.HomeDir(), image.DefaultImagesDir)
	c.Cmd.Flag("images-dir", "Local directory for storing images.").Default(defaultImagesDir).StringVar(&c.imagesDir)

	return c
}

// newImageManager creates a GitHubImageManager from the image command config.
func newImageManager(imgCmd *ImageCommand, logger log.Logger) (image.ImageManager, error) {
	mgr, err := image.NewGitHubImageManager(image.GitHubImageManagerConfig{
		Repo:       imgCmd.repo,
		ImagesDir:  imgCmd.imagesDir,
		HTTPClient: http.DefaultClient,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create image manager: %w", err)
	}
	return mgr, nil
}

// newSnapshotManager creates a LocalSnapshotManager from the image command config.
func newSnapshotManager(imgCmd *ImageCommand, logger log.Logger) (image.SnapshotManager, error) {
	mgr, err := image.NewLocalSnapshotManager(image.LocalSnapshotManagerConfig{
		ImagesDir: imgCmd.imagesDir,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create snapshot manager: %w", err)
	}
	return mgr, nil
}
