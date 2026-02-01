package commands

import (
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/sandbox/docker"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/sandbox/firecracker"
	"github.com/slok/sbx/internal/storage"
)

// newEngineFromConfig creates an engine based on the sandbox configuration.
func newEngineFromConfig(cfg model.SandboxConfig, taskRepo storage.TaskRepository, logger log.Logger) (sandbox.Engine, error) {
	if cfg.DockerEngine != nil {
		return docker.NewEngine(docker.EngineConfig{
			TaskRepo: taskRepo,
			Logger:   logger,
		})
	}

	if cfg.FirecrackerEngine != nil {
		return firecracker.NewEngine(firecracker.EngineConfig{
			TaskRepo: taskRepo,
			Logger:   logger,
		})
	}

	// Fallback to fake engine (for backward compatibility or testing)
	return fake.NewEngine(fake.EngineConfig{
		TaskRepo: taskRepo,
		Logger:   logger,
	})
}
