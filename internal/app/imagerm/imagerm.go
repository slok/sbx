package imagerm

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/log"
)

// ServiceConfig is the configuration for the image remove service.
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

// Service handles removing installed images.
type Service struct {
	manager image.ImageManager
	logger  log.Logger
}

// NewService creates a new image remove service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &Service{
		manager: cfg.Manager,
		logger:  cfg.Logger,
	}, nil
}

// Request is the remove request parameters.
type Request struct {
	Version string
}

// Run removes a locally installed image.
func (s *Service) Run(ctx context.Context, req Request) error {
	if err := s.manager.Remove(ctx, req.Version); err != nil {
		return fmt.Errorf("removing image %s: %w", req.Version, err)
	}
	return nil
}
