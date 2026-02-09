package create_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func validConfig() model.SandboxConfig {
	return model.SandboxConfig{
		Name: "test-sandbox",
		FirecrackerEngine: &model.FirecrackerEngineConfig{
			RootFS:      "/images/rootfs.ext4",
			KernelImage: "/images/vmlinux",
		},
		Resources: model.Resources{VCPUs: 2, MemoryMB: 2048, DiskGB: 10},
	}
}

func TestCreateService(t *testing.T) {
	t.Run("successful create", func(t *testing.T) {
		eng := sandboxmock.NewMockEngine(t)
		repo := storagemock.NewMockRepository(t)

		repo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return((*model.Sandbox)(nil), model.ErrNotFound)
		eng.On("Create", mock.Anything, mock.Anything).Return(&model.Sandbox{ID: "01", Name: "test-sandbox", Status: model.SandboxStatusCreated, Config: validConfig()}, nil)
		repo.On("CreateSandbox", mock.Anything, mock.Anything).Return(nil)

		svc, err := create.NewService(create.ServiceConfig{Engine: eng, Repository: repo, Logger: log.Noop})
		require.NoError(t, err)

		sb, err := svc.Create(context.Background(), create.CreateOptions{Config: validConfig()})
		require.NoError(t, err)
		require.NotNil(t, sb)
		assert.Equal(t, "test-sandbox", sb.Name)
	})

	t.Run("invalid config", func(t *testing.T) {
		eng := sandboxmock.NewMockEngine(t)
		repo := storagemock.NewMockRepository(t)
		svc, err := create.NewService(create.ServiceConfig{Engine: eng, Repository: repo, Logger: log.Noop})
		require.NoError(t, err)

		cfg := validConfig()
		cfg.FirecrackerEngine.RootFS = ""

		sb, err := svc.Create(context.Background(), create.CreateOptions{Config: cfg})
		require.Error(t, err)
		assert.Nil(t, sb)
	})

	t.Run("name conflict", func(t *testing.T) {
		eng := sandboxmock.NewMockEngine(t)
		repo := storagemock.NewMockRepository(t)
		repo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(&model.Sandbox{ID: "existing"}, nil)

		svc, err := create.NewService(create.ServiceConfig{Engine: eng, Repository: repo, Logger: log.Noop})
		require.NoError(t, err)

		sb, err := svc.Create(context.Background(), create.CreateOptions{Config: validConfig()})
		require.Error(t, err)
		assert.Nil(t, sb)
	})

	t.Run("engine failure", func(t *testing.T) {
		eng := sandboxmock.NewMockEngine(t)
		repo := storagemock.NewMockRepository(t)
		repo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return((*model.Sandbox)(nil), model.ErrNotFound)
		eng.On("Create", mock.Anything, mock.Anything).Return((*model.Sandbox)(nil), errors.New("boom"))

		svc, err := create.NewService(create.ServiceConfig{Engine: eng, Repository: repo, Logger: log.Noop})
		require.NoError(t, err)

		sb, err := svc.Create(context.Background(), create.CreateOptions{Config: validConfig()})
		require.Error(t, err)
		assert.Nil(t, sb)
	})

}
