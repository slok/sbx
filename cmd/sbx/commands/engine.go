package commands

import (
	"fmt"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/sandbox/docker"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/task"
)

// newEngineFromConfig creates an engine based on the sandbox configuration.
func newEngineFromConfig(cfg model.SandboxConfig, taskMgr task.Manager, logger log.Logger) (sandbox.Engine, error) {
	if cfg.DockerEngine != nil {
		return docker.NewEngine(docker.EngineConfig{
			TaskMgr: taskMgr,
			Logger:  logger,
		})
	}

	if cfg.FirecrackerEngine != nil {
		return nil, fmt.Errorf("firecracker engine not yet implemented")
	}

	// Fallback to fake engine (for backward compatibility or testing)
	return fake.NewEngine(fake.EngineConfig{
		TaskMgr: taskMgr,
		Logger:  logger,
	})
}
