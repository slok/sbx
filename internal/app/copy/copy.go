package copy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the copy service.
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
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "app.Copy"})
	return nil
}

// Service handles file copy operations to/from sandboxes.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new copy service.
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

// Request contains the parameters for a copy operation.
type Request struct {
	Source      string // Source path (with optional sandbox: prefix)
	Destination string // Destination path (with optional sandbox: prefix)
}

// ParsedCopy contains the parsed copy operation details.
type ParsedCopy struct {
	SandboxRef string // Name or ID of the sandbox
	LocalPath  string // Path on the host
	RemotePath string // Path in the sandbox
	ToSandbox  bool   // true = host->sandbox, false = sandbox->host
}

// ParseCopyArgs parses the source and destination arguments to determine
// the copy direction and extract sandbox reference and paths.
func ParseCopyArgs(src, dst string) (*ParsedCopy, error) {
	srcHasColon := strings.Contains(src, ":")
	dstHasColon := strings.Contains(dst, ":")

	if srcHasColon && dstHasColon {
		return nil, fmt.Errorf("cannot copy between two sandboxes, one argument must be a local path")
	}
	if !srcHasColon && !dstHasColon {
		return nil, fmt.Errorf("invalid syntax, one argument must specify sandbox (e.g., my-sandbox:/path)")
	}

	if dstHasColon {
		// Host -> Sandbox (CopyTo)
		parts := strings.SplitN(dst, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid sandbox path format: %s (expected sandbox:/path)", dst)
		}
		return &ParsedCopy{
			SandboxRef: parts[0],
			LocalPath:  src,
			RemotePath: parts[1],
			ToSandbox:  true,
		}, nil
	}

	// Sandbox -> Host (CopyFrom)
	parts := strings.SplitN(src, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid sandbox path format: %s (expected sandbox:/path)", src)
	}
	return &ParsedCopy{
		SandboxRef: parts[0],
		LocalPath:  dst,
		RemotePath: parts[1],
		ToSandbox:  false,
	}, nil
}

// Run executes a copy operation.
func (s *Service) Run(ctx context.Context, req Request) error {
	// 1. Parse arguments
	parsed, err := ParseCopyArgs(req.Source, req.Destination)
	if err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	// 2. Validate local path exists (only for host -> sandbox)
	if parsed.ToSandbox {
		if _, err := os.Stat(parsed.LocalPath); os.IsNotExist(err) {
			return fmt.Errorf("source path '%s' does not exist", parsed.LocalPath)
		}
	}

	// 3. Get sandbox from storage (by name or ID)
	sbx, err := s.repo.GetSandboxByName(ctx, parsed.SandboxRef)
	if err != nil {
		// Try by ID if name lookup failed
		if errors.Is(err, model.ErrNotFound) {
			sbx, err = s.repo.GetSandbox(ctx, parsed.SandboxRef)
		}
		if err != nil {
			return fmt.Errorf("could not find sandbox '%s': %w", parsed.SandboxRef, err)
		}
	}

	// 4. Validate sandbox is running
	if sbx.Status != model.SandboxStatusRunning {
		return fmt.Errorf("sandbox '%s' is not running (status: %s)", sbx.Name, sbx.Status)
	}

	// 5. Execute copy operation
	if parsed.ToSandbox {
		s.logger.Infof("Copying %s to %s:%s", parsed.LocalPath, sbx.Name, parsed.RemotePath)
		if err := s.engine.CopyTo(ctx, sbx.ID, parsed.LocalPath, parsed.RemotePath); err != nil {
			return fmt.Errorf("could not copy to sandbox: %w", err)
		}
	} else {
		s.logger.Infof("Copying %s:%s to %s", sbx.Name, parsed.RemotePath, parsed.LocalPath)
		if err := s.engine.CopyFrom(ctx, sbx.ID, parsed.RemotePath, parsed.LocalPath); err != nil {
			return fmt.Errorf("could not copy from sandbox: %w", err)
		}
	}

	return nil
}
