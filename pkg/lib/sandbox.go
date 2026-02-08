package lib

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/app/list"
	"github.com/slok/sbx/internal/app/remove"
	"github.com/slok/sbx/internal/app/start"
	"github.com/slok/sbx/internal/app/status"
	"github.com/slok/sbx/internal/app/stop"
	"github.com/slok/sbx/internal/model"
)

// CreateSandbox creates a new sandbox.
func (c *Client) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*Sandbox, error) {
	cfg := toInternalSandboxConfig(opts)

	// For fake engine, provide stub paths so validation passes.
	if opts.Engine == EngineFake && cfg.FirecrackerEngine == nil {
		cfg.FirecrackerEngine = &model.FirecrackerEngineConfig{
			RootFS:      "/fake/rootfs.ext4",
			KernelImage: "/fake/vmlinux",
		}
	}

	eng, err := c.newEngineForCreate(opts.Engine, "")
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := create.NewService(create.ServiceConfig{
		Engine:     eng,
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	sb, err := svc.Create(ctx, create.CreateOptions{
		Config:       cfg,
		FromSnapshot: opts.FromSnapshot,
	})
	if err != nil {
		return nil, mapError(err)
	}

	result := fromInternalSandbox(*sb)
	return &result, nil
}

// StartSandbox starts a stopped or created sandbox.
// Pass nil opts for defaults.
func (c *Client) StartSandbox(ctx context.Context, nameOrID string, opts *StartSandboxOpts) (*Sandbox, error) {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return nil, mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := start.NewService(start.ServiceConfig{
		Engine:     eng,
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, start.Request{
		NameOrID:      nameOrID,
		SessionConfig: toInternalSessionConfig(opts),
	})
	if err != nil {
		return nil, mapError(err)
	}

	out := fromInternalSandbox(*result)
	return &out, nil
}

// StopSandbox stops a running sandbox.
func (c *Client) StopSandbox(ctx context.Context, nameOrID string) (*Sandbox, error) {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return nil, mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := stop.NewService(stop.ServiceConfig{
		Engine:     eng,
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, stop.Request{
		NameOrID: nameOrID,
	})
	if err != nil {
		return nil, mapError(err)
	}

	out := fromInternalSandbox(*result)
	return &out, nil
}

// RemoveSandbox removes a sandbox. If force is true, a running sandbox is stopped first.
func (c *Client) RemoveSandbox(ctx context.Context, nameOrID string, force bool) (*Sandbox, error) {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return nil, mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := remove.NewService(remove.ServiceConfig{
		Engine:     eng,
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, remove.Request{
		NameOrID: nameOrID,
		Force:    force,
	})
	if err != nil {
		return nil, mapError(err)
	}

	out := fromInternalSandbox(*result)
	return &out, nil
}

// ListSandboxes lists sandboxes with optional filtering. Pass nil opts for all sandboxes.
func (c *Client) ListSandboxes(ctx context.Context, opts *ListSandboxesOpts) ([]Sandbox, error) {
	svc, err := list.NewService(list.ServiceConfig{
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, list.Request{
		StatusFilter: toInternalStatusFilter(opts),
	})
	if err != nil {
		return nil, mapError(err)
	}

	return fromInternalSandboxList(result), nil
}

// GetSandbox retrieves a sandbox by name or ID.
func (c *Client) GetSandbox(ctx context.Context, nameOrID string) (*Sandbox, error) {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return nil, mapError(err)
	}

	out := fromInternalSandbox(*sb)
	return &out, nil
}

// getInternalSandbox resolves a sandbox from storage by name or ID.
func (c *Client) getInternalSandbox(ctx context.Context, nameOrID string) (*model.Sandbox, error) {
	svc, err := status.NewService(status.ServiceConfig{
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	return svc.Run(ctx, status.Request{
		NameOrID: nameOrID,
	})
}
