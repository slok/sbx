package exec

import (
	"context"
	"errors"
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the exec service.
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
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.Exec"})
	return nil
}

// Service handles command execution in sandboxes.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new exec service.
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

// Request contains the parameters for executing a command.
type Request struct {
	NameOrID string
	Command  []string
	Opts     model.ExecOpts
}

// Run executes a command in a sandbox.
func (s *Service) Run(ctx context.Context, req Request) (*model.ExecResult, error) {
	// 1. Validate command
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("command cannot be empty: %w", model.ErrNotValid)
	}

	// 2. Get sandbox from storage (by name or ID)
	sandbox, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if err != nil {
		// Try by ID if name lookup failed
		if errors.Is(err, model.ErrNotFound) {
			sandbox, err = s.repo.GetSandbox(ctx, req.NameOrID)
		}
		if err != nil {
			return nil, fmt.Errorf("could not find sandbox: %w", err)
		}
	}

	// 3. Validate sandbox is running
	if sandbox.Status != model.SandboxStatusRunning {
		return nil, fmt.Errorf("sandbox %s is not running (status: %s): %w", sandbox.Name, sandbox.Status, model.ErrNotValid)
	}

	// 4. Execute command via engine
	result, err := s.engine.Exec(ctx, sandbox.ID, req.Command, req.Opts)
	if err != nil {
		return nil, fmt.Errorf("could not execute command: %w", err)
	}

	s.logger.Infof("Executed command in sandbox %s (%s): exit code %d", sandbox.Name, sandbox.ID, result.ExitCode)

	return result, nil
}
