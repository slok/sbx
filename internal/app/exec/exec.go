package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	// Files are local file paths to upload into the sandbox before executing.
	// Files are uploaded to the working directory (Opts.WorkingDir) or "/" if unset.
	Files []string
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

	// 4. Upload files before exec (if any).
	if len(req.Files) > 0 {
		destDir := req.Opts.WorkingDir
		if destDir == "" {
			destDir = "/"
		}

		// Validate all local files exist before doing any work.
		for _, f := range req.Files {
			if _, err := os.Stat(f); err != nil {
				return nil, fmt.Errorf("upload file %q does not exist: %w: %w", f, err, model.ErrNotValid)
			}
		}

		// Ensure the destination directory exists inside the sandbox.
		if _, err := s.engine.Exec(ctx, sandbox.ID, []string{"mkdir", "-p", destDir}, model.ExecOpts{}); err != nil {
			return nil, fmt.Errorf("could not create destination directory %q: %w", destDir, err)
		}

		for _, f := range req.Files {
			remotePath := filepath.Join(destDir, filepath.Base(f))
			s.logger.Debugf("Uploading %s to %s:%s", f, sandbox.Name, remotePath)

			if err := s.engine.CopyTo(ctx, sandbox.ID, f, remotePath); err != nil {
				return nil, fmt.Errorf("could not upload file %q: %w", f, err)
			}
		}
	}

	// 5. Execute command via engine.
	result, err := s.engine.Exec(ctx, sandbox.ID, req.Command, req.Opts)
	if err != nil {
		return nil, fmt.Errorf("could not execute command: %w", err)
	}

	s.logger.Debugf("executed command in sandbox %s (%s): exit code %d", sandbox.Name, sandbox.ID, result.ExitCode)

	return result, nil
}
