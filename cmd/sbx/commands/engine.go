package commands

import (
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/sandbox/firecracker"
	"github.com/slok/sbx/internal/storage"
)

// newEngineFromConfig creates an engine based on the sandbox configuration.
func newEngineFromConfig(cfg model.SandboxConfig, repo storage.Repository, logger log.Logger) (sandbox.Engine, error) {
	if cfg.FirecrackerEngine != nil {
		return firecracker.NewEngine(firecracker.EngineConfig{
			Repository: repo,
			Logger:     logger,
		})
	}

	// Fallback to fake engine (for backward compatibility or testing)
	return fake.NewEngine(fake.EngineConfig{
		Logger: logger,
	})
}
