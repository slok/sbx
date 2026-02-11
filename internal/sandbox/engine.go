package sandbox

import (
	"context"

	"github.com/slok/sbx/internal/model"
)

// StartOpts contains options for starting a sandbox.
type StartOpts struct {
	// EgressPolicy is the egress control policy to enforce.
	// When nil, the sandbox has unrestricted egress (default behavior).
	EgressPolicy *model.EgressPolicy
}

// Engine is the interface for sandbox lifecycle management.
type Engine interface {
	// Check performs preflight checks and returns the results.
	// Checks verify that the engine has all required dependencies and permissions.
	Check(ctx context.Context) []model.CheckResult

	Create(ctx context.Context, cfg model.SandboxConfig) (*model.Sandbox, error)
	Start(ctx context.Context, id string, opts StartOpts) error
	Stop(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	Status(ctx context.Context, id string) (*model.Sandbox, error)
	Exec(ctx context.Context, id string, command []string, opts model.ExecOpts) (*model.ExecResult, error)

	// CopyTo copies a file or directory from the local host to the sandbox.
	// Directories are copied recursively.
	CopyTo(ctx context.Context, id string, srcLocal string, dstRemote string) error

	// CopyFrom copies a file or directory from the sandbox to the local host.
	// Directories are copied recursively.
	CopyFrom(ctx context.Context, id string, srcRemote string, dstLocal string) error

	// Forward forwards ports from localhost to the sandbox.
	// Blocks until context is cancelled or connection drops.
	// Not all engines support forwarding (e.g., Docker requires ports at creation time).
	Forward(ctx context.Context, id string, ports []model.PortMapping) error
}
