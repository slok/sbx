package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/fake"
)

func TestEngineCreateStartStopRemove(t *testing.T) {
	tests := map[string]struct {
		actions func(ctx context.Context, t *testing.T, eng *fake.Engine) error
		expErr  bool
	}{
		"Creating a sandbox should return created status": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10,
					},
				}

				sandbox, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				assert.NotEmpty(t, sandbox.ID)
				assert.Equal(t, "test", sandbox.Name)
				assert.Equal(t, model.SandboxStatusCreated, sandbox.Status)
				assert.Nil(t, sandbox.StartedAt)
				assert.Nil(t, sandbox.StoppedAt)

				return nil
			},
		},

		"Getting status of created sandbox should show created": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				status, err := eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, created.ID, status.ID)
				assert.Equal(t, model.SandboxStatusCreated, status.Status)

				return nil
			},
		},

		"Getting status of non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				_, err := eng.Status(ctx, "non-existent")
				return err
			},
			expErr: true,
		},

		"Full lifecycle (create, start, stop, start, remove) should work": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				// Create
				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusCreated, created.Status)

				// Start (first time)
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				status, err := eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusRunning, status.Status)

				// Stop
				err = eng.Stop(ctx, created.ID)
				require.NoError(t, err)

				status, err = eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusStopped, status.Status)
				assert.NotNil(t, status.StoppedAt)

				// Start again
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				status, err = eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusRunning, status.Status)

				// Remove
				err = eng.Remove(ctx, created.ID)
				require.NoError(t, err)

				// Status should fail after removal
				_, err = eng.Status(ctx, created.ID)
				assert.Error(t, err)
				assert.True(t, errors.Is(err, model.ErrNotFound))

				return nil
			},
		},

		"Starting an already running sandbox should be idempotent": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				// Start
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				// Start again (idempotent)
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				status, err := eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusRunning, status.Status)

				return nil
			},
		},

		"Stopping an already stopped sandbox should be idempotent": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				// Start then stop
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				err = eng.Stop(ctx, created.ID)
				require.NoError(t, err)

				// Stop again
				err = eng.Stop(ctx, created.ID)
				require.NoError(t, err)

				status, err := eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusStopped, status.Status)

				return nil
			},
		},

		"Starting non-existent sandbox should succeed (no-op)": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.Start(ctx, "non-existent")
			},
			expErr: false,
		},
		"Stopping non-existent sandbox should succeed (no-op)": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.Stop(ctx, "non-existent")
			},
			expErr: false,
		},
		"Removing non-existent sandbox should succeed (no-op)": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.Remove(ctx, "non-existent")
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			eng, err := fake.NewEngine(fake.EngineConfig{
				Logger: log.Noop,
			})
			require.NoError(t, err)

			err = test.actions(context.Background(), t, eng)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestEngineCopyToFrom(t *testing.T) {
	tests := map[string]struct {
		actions func(ctx context.Context, t *testing.T, eng *fake.Engine) error
		expErr  bool
	}{
		"CopyTo on running sandbox should succeed": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				return eng.CopyTo(ctx, created.ID, "/local/file.txt", "/remote/file.txt")
			},
			expErr: false,
		},

		"CopyFrom on running sandbox should succeed": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				return eng.CopyFrom(ctx, created.ID, "/remote/file.txt", "/local/file.txt")
			},
			expErr: false,
		},

		"CopyTo on stopped sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				err = eng.Stop(ctx, created.ID)
				require.NoError(t, err)

				return eng.CopyTo(ctx, created.ID, "/local/file.txt", "/remote/file.txt")
			},
			expErr: true,
		},

		"CopyFrom on stopped sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				err = eng.Start(ctx, created.ID)
				require.NoError(t, err)

				err = eng.Stop(ctx, created.ID)
				require.NoError(t, err)

				return eng.CopyFrom(ctx, created.ID, "/remote/file.txt", "/local/file.txt")
			},
			expErr: true,
		},

		"CopyTo with empty source should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				return eng.CopyTo(ctx, created.ID, "", "/remote/file.txt")
			},
			expErr: true,
		},

		"CopyTo with empty destination should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				return eng.CopyTo(ctx, created.ID, "/local/file.txt", "")
			},
			expErr: true,
		},

		"CopyFrom with empty source should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				return eng.CopyFrom(ctx, created.ID, "", "/local/file.txt")
			},
			expErr: true,
		},

		"CopyFrom with empty destination should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:         "test",
					DockerEngine: &model.DockerEngineConfig{Image: "ubuntu-22.04"},
					Resources:    model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				return eng.CopyFrom(ctx, created.ID, "/remote/file.txt", "")
			},
			expErr: true,
		},

		"CopyTo on non-existent sandbox should succeed (stateless mode)": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.CopyTo(ctx, "non-existent", "/local/file.txt", "/remote/file.txt")
			},
			expErr: false,
		},

		"CopyFrom on non-existent sandbox should succeed (stateless mode)": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.CopyFrom(ctx, "non-existent", "/remote/file.txt", "/local/file.txt")
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			eng, err := fake.NewEngine(fake.EngineConfig{
				Logger: log.Noop,
			})
			require.NoError(t, err)

			err = test.actions(context.Background(), t, eng)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}
