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

// CreateSandbox creates a new sandbox with the given configuration.
//
// The sandbox is created in [SandboxStatusCreated] state. Call [Client.StartSandbox]
// to start it. The sandbox name must be unique.
//
// For Firecracker sandboxes, provide kernel and rootfs paths via
// [CreateSandboxOpts].Firecracker. For the fake engine (testing), these are
// auto-populated with stub values.
//
// Returns [ErrAlreadyExists] if a sandbox with the same name exists,
// or [ErrNotValid] if the configuration is invalid.
func (c *Client) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*Sandbox, error) {
	cfg := toInternalSandboxConfig(opts)

	// For fake engine, provide stub paths so validation passes.
	if opts.Engine == EngineFake && cfg.FirecrackerEngine == nil {
		cfg.FirecrackerEngine = &model.FirecrackerEngineConfig{
			RootFS:      "/fake/rootfs.ext4",
			KernelImage: "/fake/vmlinux",
		}
	}

	eng, err := c.newEngineForCreate(opts.Engine)
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

// StartSandbox starts a sandbox that is in created or stopped state.
//
// Use opts to inject session environment variables that will be available
// inside the sandbox. Pass nil for defaults (no extra env vars).
//
// Returns [ErrNotFound] if the sandbox does not exist, or [ErrNotValid] if
// the sandbox is not in a startable state.
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
//
// The sandbox must be in [SandboxStatusRunning] state.
//
// Returns [ErrNotFound] if the sandbox does not exist, or [ErrNotValid] if
// the sandbox is not running.
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

// RemoveSandbox removes a sandbox and cleans up its resources.
//
// If force is false and the sandbox is running, it returns [ErrNotValid].
// If force is true, a running sandbox is stopped first (best-effort) then removed.
//
// Returns [ErrNotFound] if the sandbox does not exist.
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

// ListSandboxes returns all sandboxes, optionally filtered by status.
//
// Pass nil opts to list all sandboxes regardless of status. Use
// [ListSandboxesOpts].Status to filter by a specific [SandboxStatus].
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
//
// The nameOrID parameter is first matched against sandbox names. If no match is
// found and the value looks like a ULID, it is tried as an ID.
//
// Returns [ErrNotFound] if the sandbox does not exist.
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
