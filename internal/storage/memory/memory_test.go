package memory_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/memory"
)

func TestRepositoryCRUD(t *testing.T) {
	tests := map[string]struct {
		actions func(ctx context.Context, t *testing.T, repo *memory.Repository) error
		expErr  bool
	}{
		"Creating a sandbox should work": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				now := time.Now().UTC()
				sandbox := model.Sandbox{
					ID:        "test-id",
					Name:      "test",
					Status:    model.SandboxStatusRunning,
					CreatedAt: now,
					Config: model.SandboxConfig{
						Name: "test",
						DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
						Resources: model.Resources{
							VCPUs:    2,
							MemoryMB: 2048,
							DiskGB:   10,
						},
					},
				}

				err := repo.CreateSandbox(ctx, sandbox)
				require.NoError(t, err)

				// Verify we can retrieve it
				retrieved, err := repo.GetSandbox(ctx, "test-id")
				require.NoError(t, err)
				assert.Equal(t, "test-id", retrieved.ID)
				assert.Equal(t, "test", retrieved.Name)

				return nil
			},
		},

		"Creating duplicate ID should fail": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandbox := model.Sandbox{
					ID:     "test-id",
					Name:   "test",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					},
				}

				err := repo.CreateSandbox(ctx, sandbox)
				require.NoError(t, err)

				// Try to create with same ID
				sandbox2 := sandbox
				sandbox2.Name = "different"
				return repo.CreateSandbox(ctx, sandbox2)
			},
			expErr: true,
		},

		"Creating duplicate name should fail": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandbox := model.Sandbox{
					ID:     "test-id-1",
					Name:   "test",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					},
				}

				err := repo.CreateSandbox(ctx, sandbox)
				require.NoError(t, err)

				// Try to create with same name
				sandbox2 := sandbox
				sandbox2.ID = "test-id-2"
				return repo.CreateSandbox(ctx, sandbox2)
			},
			expErr: true,
		},

		"Getting non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				_, err := repo.GetSandbox(ctx, "non-existent")
				return err
			},
			expErr: true,
		},

		"Getting sandbox by name should work": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandbox := model.Sandbox{
					ID:     "test-id",
					Name:   "test-name",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name:      "test-name",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					},
				}

				err := repo.CreateSandbox(ctx, sandbox)
				require.NoError(t, err)

				retrieved, err := repo.GetSandboxByName(ctx, "test-name")
				require.NoError(t, err)
				assert.Equal(t, "test-id", retrieved.ID)
				assert.Equal(t, "test-name", retrieved.Name)

				return nil
			},
		},

		"Getting sandbox by non-existent name should fail": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				_, err := repo.GetSandboxByName(ctx, "non-existent")
				return err
			},
			expErr: true,
		},

		"Listing sandboxes should work": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				// Create multiple sandboxes
				for i := 0; i < 3; i++ {
					sandbox := model.Sandbox{
						ID:     fmt.Sprintf("test-id-%d", i),
						Name:   fmt.Sprintf("test-%d", i),
						Status: model.SandboxStatusRunning,
						Config: model.SandboxConfig{
							Name:      fmt.Sprintf("test-%d", i),
							Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
						},
					}
					err := repo.CreateSandbox(ctx, sandbox)
					require.NoError(t, err)
				}

				sandboxes, err := repo.ListSandboxes(ctx)
				require.NoError(t, err)
				assert.Len(t, sandboxes, 3)

				return nil
			},
		},

		"Listing empty repository should return empty slice": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandboxes, err := repo.ListSandboxes(ctx)
				require.NoError(t, err)
				assert.Empty(t, sandboxes)

				return nil
			},
		},

		"Updating a sandbox should work": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandbox := model.Sandbox{
					ID:     "test-id",
					Name:   "test",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					},
				}

				err := repo.CreateSandbox(ctx, sandbox)
				require.NoError(t, err)

				// Update status
				sandbox.Status = model.SandboxStatusStopped
				now := time.Now().UTC()
				sandbox.StoppedAt = &now

				err = repo.UpdateSandbox(ctx, sandbox)
				require.NoError(t, err)

				// Verify update
				retrieved, err := repo.GetSandbox(ctx, "test-id")
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusStopped, retrieved.Status)
				assert.NotNil(t, retrieved.StoppedAt)

				return nil
			},
		},

		"Updating non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandbox := model.Sandbox{
					ID:     "non-existent",
					Name:   "test",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					},
				}

				return repo.UpdateSandbox(ctx, sandbox)
			},
			expErr: true,
		},

		"Deleting a sandbox should work": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				sandbox := model.Sandbox{
					ID:     "test-id",
					Name:   "test",
					Status: model.SandboxStatusRunning,
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					},
				}

				err := repo.CreateSandbox(ctx, sandbox)
				require.NoError(t, err)

				err = repo.DeleteSandbox(ctx, "test-id")
				require.NoError(t, err)

				// Verify it's gone
				_, err = repo.GetSandbox(ctx, "test-id")
				assert.Error(t, err)
				assert.True(t, errors.Is(err, model.ErrNotFound))

				return nil
			},
		},

		"Deleting non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, repo *memory.Repository) error {
				return repo.DeleteSandbox(ctx, "non-existent")
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			repo, err := memory.NewRepository(memory.RepositoryConfig{
				Logger: log.Noop,
			})
			require.NoError(t, err)

			err = test.actions(context.Background(), t, repo)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}
