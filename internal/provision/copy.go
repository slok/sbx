package provision

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/log"
)

// CopyPathConfig is the configuration for creating a CopyPath provisioner.
type CopyPathConfig struct {
	// Accessor provides sandbox operations. Required.
	Accessor SandboxAccessor
	// SrcLocal is the host path to copy from. Required.
	SrcLocal string
	// DstRemote is the sandbox path to copy to. Required.
	DstRemote string
	// Logger is optional, defaults to log.Noop.
	Logger log.Logger
}

func (c *CopyPathConfig) defaults() error {
	if c.Accessor == nil {
		return fmt.Errorf("accessor is required")
	}
	if c.SrcLocal == "" {
		return fmt.Errorf("src local path is required")
	}
	if c.DstRemote == "" {
		return fmt.Errorf("dst remote path is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	return nil
}

// NewCopyPath creates a provisioner that copies a file or directory from the host
// to the sandbox. This provisioner is idempotent - it overwrites existing files/dirs.
func NewCopyPath(cfg CopyPathConfig) (Provisioner, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid copy path config: %w", err)
	}

	return ProvisionerFunc(func(ctx context.Context) error {
		cfg.Logger.Debugf("Copying %q to %q...", cfg.SrcLocal, cfg.DstRemote)

		if err := cfg.Accessor.CopyTo(ctx, cfg.SrcLocal, cfg.DstRemote); err != nil {
			return fmt.Errorf("copying %q to %q: %w", cfg.SrcLocal, cfg.DstRemote, err)
		}

		cfg.Logger.Debugf("Copied %q to %q", cfg.SrcLocal, cfg.DstRemote)
		return nil
	}), nil
}
