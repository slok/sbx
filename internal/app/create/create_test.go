package create_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/engine/enginemock"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		cfg    create.ServiceConfig
		expErr bool
		errMsg string
	}{
		"Valid config with all fields": {
			cfg: create.ServiceConfig{
				Engine:     &enginemock.MockEngine{},
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},
		"Valid config without logger uses Noop": {
			cfg: create.ServiceConfig{
				Engine:     &enginemock.MockEngine{},
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
		"Missing engine returns error": {
			cfg: create.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: true,
			errMsg: "engine is required",
		},
		"Missing repository returns error": {
			cfg: create.ServiceConfig{
				Engine: &enginemock.MockEngine{},
			},
			expErr: true,
			errMsg: "repository is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			svc, err := create.NewService(tt.cfg)

			if tt.expErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, svc)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, svc)
			}
		})
	}
}

func TestServiceCreate(t *testing.T) {
	tests := map[string]struct {
		opts        create.CreateOptions
		setupMocks  func(engine *enginemock.MockEngine, repo *storagemock.MockRepository)
		setupConfig func(t *testing.T) string
		expErr      bool
		errMsg      string
		validateRes func(t *testing.T, sb *model.Sandbox)
	}{
		"Successful creation": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "test-sandbox",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// Check name uniqueness - not found
				repo.On("GetSandboxByName", mock.Anything, "test-sandbox").
					Return((*model.Sandbox)(nil), model.ErrNotFound)

				// Engine creates sandbox
				expSandbox := &model.Sandbox{
					ID:     "01HRW9YZTEST000000000000",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name: "test-sandbox",
						Base: "ubuntu:22.04",
						Resources: model.Resources{
							VCPUs:    2,
							MemoryMB: 2048,
							DiskGB:   10240,
						},
					},
					CreatedAt: time.Now(),
				}
				eng.On("Create", mock.Anything, mock.Anything).
					Return(expSandbox, nil)

				// Repository saves sandbox
				repo.On("CreateSandbox", mock.Anything, mock.Anything).
					Return(nil)
			},
			expErr: false,
			validateRes: func(t *testing.T, sb *model.Sandbox) {
				assert.NotNil(t, sb)
				assert.Equal(t, "test-sandbox", sb.Name)
				assert.Equal(t, model.SandboxStatusRunning, sb.Status)
			},
		},
		"Name override works": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "original-name",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			opts: create.CreateOptions{
				NameOverride: "overridden-name",
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// Check for overridden name, not original
				repo.On("GetSandboxByName", mock.Anything, "overridden-name").
					Return((*model.Sandbox)(nil), model.ErrNotFound)

				expSandbox := &model.Sandbox{
					ID:     "01HRW9YZTEST000000000001",
					Name:   "overridden-name",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name: "overridden-name",
						Base: "ubuntu:22.04",
						Resources: model.Resources{
							VCPUs:    2,
							MemoryMB: 2048,
							DiskGB:   10240,
						},
					},
					CreatedAt: time.Now(),
				}
				eng.On("Create", mock.Anything, mock.MatchedBy(func(cfg model.SandboxConfig) bool {
					return cfg.Name == "overridden-name"
				})).Return(expSandbox, nil)

				repo.On("CreateSandbox", mock.Anything, mock.Anything).
					Return(nil)
			},
			expErr: false,
			validateRes: func(t *testing.T, sb *model.Sandbox) {
				assert.NotNil(t, sb)
				assert.Equal(t, "overridden-name", sb.Name)
			},
		},
		"Name conflict returns error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "existing-sandbox",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// Name already exists
				existingSandbox := &model.Sandbox{
					ID:   "01HRW9YZTEST000000000002",
					Name: "existing-sandbox",
				}
				repo.On("GetSandboxByName", mock.Anything, "existing-sandbox").
					Return(existingSandbox, nil)
			},
			expErr: true,
			errMsg: "already exists",
		},
		"Invalid YAML returns error": {
			setupConfig: func(t *testing.T) string {
				return createTestFile(t, "invalid: [yaml content")
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// No mocks needed - fails at YAML parsing
			},
			expErr: true,
			errMsg: "could not parse config",
		},
		"Missing name in config returns validation error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "", // Invalid - empty name
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// No mocks needed - fails at validation
			},
			expErr: true,
			errMsg: "invalid config",
		},
		"Missing image in config returns validation error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "test",
					Base: "", // Invalid - empty image
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// No mocks needed - fails at validation
			},
			expErr: true,
			errMsg: "invalid config",
		},
		"Invalid resource values return validation error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "test",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    0, // Invalid - zero CPUs
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// No mocks needed - fails at validation
			},
			expErr: true,
			errMsg: "invalid config",
		},
		"Engine error returns error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "test-sandbox",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				repo.On("GetSandboxByName", mock.Anything, "test-sandbox").
					Return((*model.Sandbox)(nil), model.ErrNotFound)

				// Engine fails
				eng.On("Create", mock.Anything, mock.Anything).
					Return((*model.Sandbox)(nil), errors.New("engine creation failed"))
			},
			expErr: true,
			errMsg: "could not create sandbox",
		},
		"Repository save error returns error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "test-sandbox",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				repo.On("GetSandboxByName", mock.Anything, "test-sandbox").
					Return((*model.Sandbox)(nil), model.ErrNotFound)

				expSandbox := &model.Sandbox{
					ID:     "01HRW9YZTEST000000000003",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				eng.On("Create", mock.Anything, mock.Anything).
					Return(expSandbox, nil)

				// Repository save fails
				repo.On("CreateSandbox", mock.Anything, mock.Anything).
					Return(errors.New("database error"))
			},
			expErr: true,
			errMsg: "could not save sandbox",
		},
		"Repository check error returns error": {
			setupConfig: func(t *testing.T) string {
				return createTestConfig(t, model.SandboxConfig{
					Name: "test-sandbox",
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10240,
					},
				})
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// Repository check fails with unexpected error
				repo.On("GetSandboxByName", mock.Anything, "test-sandbox").
					Return((*model.Sandbox)(nil), errors.New("database connection error"))
			},
			expErr: true,
			errMsg: "could not check name uniqueness",
		},
		"File not found returns error": {
			setupConfig: func(t *testing.T) string {
				return "/nonexistent/path/to/config.yaml"
			},
			setupMocks: func(eng *enginemock.MockEngine, repo *storagemock.MockRepository) {
				// No mocks needed - fails at file read
			},
			expErr: true,
			errMsg: "could not read config file",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Setup mocks
			mockEngine := enginemock.NewMockEngine(t)
			mockRepo := storagemock.NewMockRepository(t)
			tt.setupMocks(mockEngine, mockRepo)

			// Create service
			svc, err := create.NewService(create.ServiceConfig{
				Engine:     mockEngine,
				Repository: mockRepo,
				Logger:     log.Noop,
			})
			require.NoError(t, err)

			// Setup config file
			configPath := tt.setupConfig(t)
			opts := tt.opts
			opts.ConfigPath = configPath

			// Execute
			result, err := svc.Create(context.Background(), opts)

			// Verify
			if tt.expErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				if tt.validateRes != nil {
					tt.validateRes(t, result)
				}
			}
		})
	}
}

// Helper function to create a test config file
func createTestConfig(t *testing.T, cfg model.SandboxConfig) string {
	t.Helper()

	content := "name: " + cfg.Name + "\n"
	content += "base: " + cfg.Base + "\n"
	content += "resources:\n"
	content += "  vcpus: " + fmt.Sprintf("%d", cfg.Resources.VCPUs) + "\n"
	content += "  memory_mb: " + fmt.Sprintf("%d", cfg.Resources.MemoryMB) + "\n"
	content += "  disk_gb: " + fmt.Sprintf("%d", cfg.Resources.DiskGB) + "\n"

	return createTestFile(t, content)
}

// Helper function to create a test file with content
func createTestFile(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	return configPath
}
