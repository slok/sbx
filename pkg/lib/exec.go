package lib

import (
	"context"
	"fmt"

	appexec "github.com/slok/sbx/internal/app/exec"
	"github.com/slok/sbx/internal/model"
)

// Exec executes a command inside a running sandbox and returns the result.
//
// The command must be non-empty. Use opts to configure working directory,
// environment variables, and I/O streams. Pass nil opts for defaults
// (no working dir, no extra env, discarded output).
//
// The sandbox must be in [SandboxStatusRunning] state.
//
// Returns [ErrNotFound] if the sandbox does not exist, or [ErrNotValid] if
// the sandbox is not running or the command is empty.
func (c *Client) Exec(ctx context.Context, nameOrID string, command []string, opts *ExecOpts) (*ExecResult, error) {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return nil, mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := appexec.NewService(appexec.ServiceConfig{
		Engine:     eng,
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, appexec.Request{
		NameOrID: nameOrID,
		Command:  command,
		Opts:     toInternalExecOpts(opts),
	})
	if err != nil {
		return nil, mapError(err)
	}

	return &ExecResult{ExitCode: result.ExitCode}, nil
}

// CopyTo copies a local file or directory from the host into a running sandbox.
//
// The sandbox must be in [SandboxStatusRunning] state.
// For Firecracker sandboxes, this uses SCP over the VM's internal IP.
//
// Returns [ErrNotFound] if the sandbox does not exist, or [ErrNotValid] if
// the sandbox is not running.
func (c *Client) CopyTo(ctx context.Context, nameOrID string, srcLocal, dstRemote string) error {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return mapError(fmt.Errorf("could not create engine: %w", err))
	}

	if sb.Status != model.SandboxStatusRunning {
		return mapError(fmt.Errorf("sandbox %s is not running (status: %s): %w", sb.Name, sb.Status, ErrNotValid))
	}

	if err := eng.CopyTo(ctx, sb.ID, srcLocal, dstRemote); err != nil {
		return mapError(fmt.Errorf("could not copy to sandbox: %w", err))
	}

	return nil
}

// CopyFrom copies a file or directory from a running sandbox to the local host.
//
// The sandbox must be in [SandboxStatusRunning] state.
// For Firecracker sandboxes, this uses SCP over the VM's internal IP.
//
// Returns [ErrNotFound] if the sandbox does not exist, or [ErrNotValid] if
// the sandbox is not running.
func (c *Client) CopyFrom(ctx context.Context, nameOrID string, srcRemote, dstLocal string) error {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return mapError(fmt.Errorf("could not create engine: %w", err))
	}

	if sb.Status != model.SandboxStatusRunning {
		return mapError(fmt.Errorf("sandbox %s is not running (status: %s): %w", sb.Name, sb.Status, ErrNotValid))
	}

	if err := eng.CopyFrom(ctx, sb.ID, srcRemote, dstLocal); err != nil {
		return mapError(fmt.Errorf("could not copy from sandbox: %w", err))
	}

	return nil
}
