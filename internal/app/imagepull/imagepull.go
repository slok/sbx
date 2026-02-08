package imagepull

import (
	"context"
	"fmt"
	"io"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
)

// ServiceConfig is the configuration for the image pull service.
type ServiceConfig struct {
	Manager image.ImageManager
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

// Service handles pulling image releases.
type Service struct {
	manager image.ImageManager
	logger  log.Logger
}

// NewService creates a new image pull service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Service{manager: cfg.Manager, logger: cfg.Logger}, nil
}

// Request is the pull request parameters.
type Request struct {
	Version      string
	Force        bool
	StatusWriter io.Writer
}

// Run pulls an image release.
func (s *Service) Run(ctx context.Context, req Request) (*image.PullResult, error) {
	result, err := s.manager.Pull(ctx, req.Version, image.PullOptions{
		Force:        req.Force,
		StatusWriter: req.StatusWriter,
	})
	if err != nil {
		return nil, fmt.Errorf("pulling image %s: %w", req.Version, err)
	}
	return result, nil
}
