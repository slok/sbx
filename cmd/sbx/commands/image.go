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

// newLocalImageManager creates a LocalImageManager for local image operations.
func newLocalImageManager(imgCmd *ImageCommand, logger log.Logger) (image.ImageManager, error) {
	mgr, err := image.NewLocalImageManager(image.LocalImageManagerConfig{
		ImagesDir: imgCmd.imagesDir,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create local image manager: %w", err)
	}
	return mgr, nil
}

// newImagePuller creates a GitHubImagePuller for remote image operations.
func newImagePuller(imgCmd *ImageCommand, logger log.Logger) (image.ImagePuller, error) {
	p, err := image.NewGitHubImagePuller(image.GitHubImagePullerConfig{
		Repo:       imgCmd.repo,
		ImagesDir:  imgCmd.imagesDir,
		HTTPClient: http.DefaultClient,
		Logger:     logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create image puller: %w", err)
	}
	return p, nil
}
