package start

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/storage"
)

// ServiceConfig is the configuration for the start service.
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

	return nil
}

// Service starts a stopped sandbox.
type Service struct {
	engine sandbox.Engine
	repo   storage.Repository
	logger log.Logger
}

// NewService creates a new start service.
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

// Request represents the start request parameters.
type Request struct {
	// NameOrID is the sandbox name or ID to start.
	NameOrID string
	// SessionConfig is the optional session configuration applied at start time.
	SessionConfig model.SessionConfig
}

// Run starts a sandbox by name or ID.
// It validates the sandbox is created or stopped before attempting to start it.
func (s *Service) Run(ctx context.Context, req Request) (*model.Sandbox, error) {
	s.logger.Debugf("starting sandbox: %s", req.NameOrID)

	// Lookup sandbox by name first, then by ID if it looks like a ULID.
	sandbox, err := s.repo.GetSandboxByName(ctx, req.NameOrID)
	if errors.Is(err, model.ErrNotFound) && looksLikeULID(req.NameOrID) {
		sandbox, err = s.repo.GetSandbox(ctx, req.NameOrID)
	}
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, fmt.Errorf("sandbox not found: %s: %w", req.NameOrID, model.ErrNotFound)
		}
		return nil, fmt.Errorf("could not get sandbox: %w", err)
	}

	// Validate sandbox is in a startable state (stopped).
	if sandbox.Status != model.SandboxStatusStopped {
		return nil, fmt.Errorf("cannot start sandbox: not in startable state (current status: %s): %w", sandbox.Status, model.ErrNotValid)
	}

	sessionCfg := normalizeSessionConfig(req.SessionConfig)

	// Start the sandbox via engine.
	if err := s.engine.Start(ctx, sandbox.ID); err != nil {
		return nil, fmt.Errorf("could not start sandbox: %w", err)
	}

	if err := s.applySessionEnvToSandbox(ctx, sandbox.ID, sessionCfg.Env); err != nil {
		if stopErr := s.engine.Stop(ctx, sandbox.ID); stopErr != nil {
			s.logger.Warningf("could not stop sandbox after env setup failure: %v", stopErr)
		}
		return nil, fmt.Errorf("could not apply session environment: %w", err)
	}

	// Update sandbox state in repository.
	now := time.Now().UTC()
	sandbox.Status = model.SandboxStatusRunning
	sandbox.StartedAt = &now
	sandbox.StoppedAt = nil

	if err := s.repo.UpdateSandbox(ctx, *sandbox); err != nil {
		return nil, fmt.Errorf("could not update sandbox: %w", err)
	}

	s.logger.Infof("started sandbox: %s (ID: %s)", sandbox.Name, sandbox.ID)
	return sandbox, nil
}

func normalizeSessionConfig(cfg model.SessionConfig) model.SessionConfig {
	normalized := model.SessionConfig{
		Name: cfg.Name,
		Env:  map[string]string{},
	}

	for k, v := range cfg.Env {
		normalized.Env[k] = v
	}

	return normalized
}

func (s *Service) applySessionEnvToSandbox(ctx context.Context, sandboxID string, env map[string]string) error {
	if _, err := s.engine.Exec(ctx, sandboxID, []string{"mkdir", "-p", "/etc/sbx", "/etc/profile.d", "/root/.ssh"}, model.ExecOpts{}); err != nil {
		return fmt.Errorf("could not create session env directories: %w", err)
	}

	tmpSessionFile, err := os.CreateTemp("", "sbx-session-env-*.sh")
	if err != nil {
		return fmt.Errorf("could not create temporary session env file: %w", err)
	}
	tmpSessionPath := tmpSessionFile.Name()
	defer os.Remove(tmpSessionPath)

	if _, err := tmpSessionFile.WriteString(renderSessionEnvScript(env)); err != nil {
		tmpSessionFile.Close()
		return fmt.Errorf("could not write temporary session env file: %w", err)
	}
	if err := tmpSessionFile.Close(); err != nil {
		return fmt.Errorf("could not close temporary session env file: %w", err)
	}

	tmpProfileHookFile, err := os.CreateTemp("", "sbx-profile-hook-*.sh")
	if err != nil {
		return fmt.Errorf("could not create temporary profile hook file: %w", err)
	}
	tmpProfileHookPath := tmpProfileHookFile.Name()
	defer os.Remove(tmpProfileHookPath)

	if _, err := tmpProfileHookFile.WriteString(profileHookScript); err != nil {
		tmpProfileHookFile.Close()
		return fmt.Errorf("could not write temporary profile hook file: %w", err)
	}
	if err := tmpProfileHookFile.Close(); err != nil {
		return fmt.Errorf("could not close temporary profile hook file: %w", err)
	}

	tmpSSHRCFile, err := os.CreateTemp("", "sbx-sshrc-*")
	if err != nil {
		return fmt.Errorf("could not create temporary ssh rc file: %w", err)
	}
	tmpSSHRCPath := tmpSSHRCFile.Name()
	defer os.Remove(tmpSSHRCPath)

	if _, err := tmpSSHRCFile.WriteString(sshRCScript); err != nil {
		tmpSSHRCFile.Close()
		return fmt.Errorf("could not write temporary ssh rc file: %w", err)
	}
	if err := tmpSSHRCFile.Close(); err != nil {
		return fmt.Errorf("could not close temporary ssh rc file: %w", err)
	}

	if err := s.engine.CopyTo(ctx, sandboxID, tmpSessionPath, "/etc/sbx/session-env.sh"); err != nil {
		return fmt.Errorf("could not copy session env script: %w", err)
	}

	if err := s.engine.CopyTo(ctx, sandboxID, tmpProfileHookPath, "/etc/profile.d/sbx-session-env.sh"); err != nil {
		return fmt.Errorf("could not copy profile hook script: %w", err)
	}

	if err := s.engine.CopyTo(ctx, sandboxID, tmpSSHRCPath, "/root/.ssh/rc"); err != nil {
		return fmt.Errorf("could not copy ssh rc script: %w", err)
	}

	if _, err := s.engine.Exec(ctx, sandboxID, []string{"chmod", "644", "/etc/sbx/session-env.sh", "/etc/profile.d/sbx-session-env.sh"}, model.ExecOpts{}); err != nil {
		return fmt.Errorf("could not set session env script permissions: %w", err)
	}

	if _, err := s.engine.Exec(ctx, sandboxID, []string{"chmod", "700", "/root/.ssh/rc"}, model.ExecOpts{}); err != nil {
		return fmt.Errorf("could not set ssh rc permissions: %w", err)
	}

	return nil
}

func renderSessionEnvScript(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Managed by sbx.\n")

	for _, k := range keys {
		b.WriteString("export ")
		b.WriteString(k)
		b.WriteString("='")
		b.WriteString(escapeShellSingleQuoted(env[k]))
		b.WriteString("'\n")
	}

	return b.String()
}

func escapeShellSingleQuoted(v string) string {
	return strings.ReplaceAll(v, "'", `'"'"'`)
}

const profileHookScript = `#!/bin/sh
[ -f /etc/sbx/session-env.sh ] && . /etc/sbx/session-env.sh
`

const sshRCScript = `#!/bin/sh
[ -f /etc/sbx/session-env.sh ] && . /etc/sbx/session-env.sh
`

// looksLikeULID checks if a string looks like a ULID (26 characters, alphanumeric uppercase).
func looksLikeULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}
