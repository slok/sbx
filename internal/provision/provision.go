package provision

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
)

// Provisioner is the interface that all provisioners must implement.
// Implementations MUST be idempotent - calling Provision N times must produce the same result.
type Provisioner interface {
	Provision(ctx context.Context) error
}

// ProvisionerFunc is a convenience adapter to allow the use of ordinary functions as Provisioners.
type ProvisionerFunc func(ctx context.Context) error

func (f ProvisionerFunc) Provision(ctx context.Context) error { return f(ctx) }

// SandboxAccessor provides provisioners access to sandbox operations.
// This is a minimal interface that abstracts the sandbox engine, so provisioners
// are engine-agnostic and don't have access to lifecycle operations (start/stop/remove).
type SandboxAccessor interface {
	Exec(ctx context.Context, command []string, opts model.ExecOpts) (*model.ExecResult, error)
	CopyTo(ctx context.Context, srcLocal string, dstRemote string) error
}

// NewSandboxAccessor creates a SandboxAccessor bound to a specific sandbox ID.
// This wraps a sandbox.Engine and pre-binds the sandbox ID so provisioners
// don't need to know which sandbox they are operating on.
func NewSandboxAccessor(engine sandbox.Engine, sandboxID string) SandboxAccessor {
	return &sandboxAccessor{
		engine:    engine,
		sandboxID: sandboxID,
	}
}

type sandboxAccessor struct {
	engine    sandbox.Engine
	sandboxID string
}

func (a *sandboxAccessor) Exec(ctx context.Context, command []string, opts model.ExecOpts) (*model.ExecResult, error) {
	return a.engine.Exec(ctx, a.sandboxID, command, opts)
}

func (a *sandboxAccessor) CopyTo(ctx context.Context, srcLocal string, dstRemote string) error {
	return a.engine.CopyTo(ctx, a.sandboxID, srcLocal, dstRemote)
}

// NewProvisionerChain returns a Provisioner that runs all provisioners sequentially.
// If any provisioner fails, the chain stops and returns the error.
// An empty chain succeeds immediately.
func NewProvisionerChain(provisioners ...Provisioner) Provisioner {
	return ProvisionerFunc(func(ctx context.Context) error {
		for i, p := range provisioners {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("provisioner chain cancelled at step %d: %w", i, err)
			}

			if err := p.Provision(ctx); err != nil {
				return fmt.Errorf("provisioner chain failed at step %d: %w", i, err)
			}
		}
		return nil
	})
}

// NewNoopProvisioner returns a provisioner that does nothing.
func NewNoopProvisioner() Provisioner {
	return ProvisionerFunc(func(_ context.Context) error { return nil })
}

// NewLogProvisioner wraps a provisioner with debug logging before and after execution.
func NewLogProvisioner(name string, logger log.Logger, p Provisioner) Provisioner {
	return ProvisionerFunc(func(ctx context.Context) error {
		logger.Debugf("Provisioning %q...", name)

		if err := p.Provision(ctx); err != nil {
			return err
		}

		logger.Debugf("Provisioned %q", name)
		return nil
	})
}
