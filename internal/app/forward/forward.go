package forward

import (
	"context"
	"errors"
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the forward service.
type ServiceConfig struct {
	Engine     sandbox.Engine
	Repository storage.Repository
	Logger     log.Logger
}

func (c *ServiceConfig) defaults() error {
	if c.Engine == nil {
		return fmt.Errorf("engine is required")
	}
	if c.Repository == nil {
		return fmt.Errorf("repository is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.Forward"})
	return nil
}

// Service handles port forwarding to sandboxes.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new forward service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		engine: cfg.Engine,
		repo:   cfg.Repository,
		logger: cfg.Logger,
	}, nil
}

// Request contains the parameters for port forwarding.
type Request struct {
	NameOrID string
	Ports    []model.PortMapping
}

// Run starts port forwarding to a sandbox.
// Blocks until context is cancelled or connection drops.
func (s *Service) Run(ctx context.Context, req Request) error {
	// 1. Validate ports
	if len(req.Ports) == 0 {
		return fmt.Errorf("at least one port mapping is required: %w", model.ErrNotValid)
	}

	// 2. Get sandbox from storage (by name or ID)
	sbx, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if err != nil {
		// Try by ID if name lookup failed
		if errors.Is(err, model.ErrNotFound) {
			sbx, err = s.repo.GetSandbox(ctx, req.NameOrID)
		}
		if err != nil {
			return fmt.Errorf("could not find sandbox: %w", err)
		}
	}

	// 3. Validate sandbox is running
	if sbx.Status != model.SandboxStatusRunning {
		return fmt.Errorf("sandbox %s is not running (status: %s): %w", sbx.Name, sbx.Status, model.ErrNotValid)
	}

	s.logger.Debugf("Starting port forwarding to sandbox %s (%s)", sbx.Name, sbx.ID)
	for _, pm := range req.Ports {
		s.logger.Debugf("  localhost:%d -> sandbox:%d", pm.LocalPort, pm.RemotePort)
	}

	// 4. Forward ports via engine (blocks until context cancelled)
	if err := s.engine.Forward(ctx, sbx.ID, req.Ports); err != nil {
		// Context cancellation is expected behavior
		if errors.Is(err, context.Canceled) {
			s.logger.Debugf("Port forwarding stopped")
			return nil
		}
		return fmt.Errorf("port forwarding failed: %w", err)
	}

	return nil
}
