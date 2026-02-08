package lib

import (
	"context"
	"fmt"

	appexec "github.com/slok/sbx/internal/app/exec"
	"github.com/slok/sbx/internal/model"
)

// Exec executes a command inside a running sandbox.
// Pass nil opts for defaults.
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

// CopyTo copies a local file or directory into a running sandbox.
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
