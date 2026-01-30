package fake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/engine/fake"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

func TestEngineCreateStartStopRemove(t *testing.T) {
	tests := map[string]struct {
		actions func(ctx context.Context, t *testing.T, eng *fake.Engine) error
		expErr  bool
	}{
		"Creating a sandbox should work": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name: "test",
					Base: "ubuntu-22.04",
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
				assert.Equal(t, model.SandboxStatusRunning, sandbox.Status)
				assert.NotNil(t, sandbox.StartedAt)
				assert.Nil(t, sandbox.StoppedAt)

				return nil
			},
		},

		"Getting status of created sandbox should work": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:      "test",
					Base:      "ubuntu-22.04",
					Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				status, err := eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, created.ID, status.ID)
				assert.Equal(t, model.SandboxStatusRunning, status.Status)

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

		"Full lifecycle (create, stop, start, remove) should work": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				cfg := model.SandboxConfig{
					Name:      "test",
					Base:      "ubuntu-22.04",
					Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				// Create
				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusRunning, created.Status)

				// Stop
				err = eng.Stop(ctx, created.ID)
				require.NoError(t, err)

				status, err := eng.Status(ctx, created.ID)
				require.NoError(t, err)
				assert.Equal(t, model.SandboxStatusStopped, status.Status)
				assert.NotNil(t, status.StoppedAt)

				// Start
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
					Name:      "test",
					Base:      "ubuntu-22.04",
					Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
				require.NoError(t, err)

				// Start again
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
					Name:      "test",
					Base:      "ubuntu-22.04",
					Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				}

				created, err := eng.Create(ctx, cfg)
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

		"Starting non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.Start(ctx, "non-existent")
			},
			expErr: true,
		},

		"Stopping non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.Stop(ctx, "non-existent")
			},
			expErr: true,
		},

		"Removing non-existent sandbox should fail": {
			actions: func(ctx context.Context, t *testing.T, eng *fake.Engine) error {
				return eng.Remove(ctx, "non-existent")
			},
			expErr: true,
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
