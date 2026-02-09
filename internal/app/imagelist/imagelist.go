package imagelist

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

// ServiceConfig is the configuration for the image list service.
type ServiceConfig struct {
	Manager image.ImageManager
	Puller  image.ImagePuller
	Logger  log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Manager == nil {
		return fmt.Errorf("image manager is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	return nil
}

// Service handles listing image releases.
type Service struct {
	manager image.ImageManager
	puller  image.ImagePuller
	logger  log.Logger
}

// NewService creates a new image list service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Service{
		manager: cfg.Manager,
		puller:  cfg.Puller,
		logger:  cfg.Logger,
	}, nil
}

// Run lists available images (local + remote).
func (s *Service) Run(ctx context.Context) ([]model.ImageRelease, error) {
	// Get locally installed images (releases and snapshots).
	localImages, err := s.manager.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing local images: %w", err)
	}

	// Build a set of installed image names for fast lookup.
	installed := make(map[string]struct{}, len(localImages))
	for _, img := range localImages {
		installed[img.Version] = struct{}{}
	}

	// Merge with remote releases if puller is available.
	if s.puller != nil {
		remoteReleases, err := s.puller.ListRemote(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing remote releases: %w", err)
		}

		for _, r := range remoteReleases {
			if _, ok := installed[r.Version]; ok {
				// Already in local list as installed, skip the remote entry.
				continue
			}
			// Add remote-only release (not installed locally).
			localImages = append(localImages, r)
		}
	}

	return localImages, nil
}
