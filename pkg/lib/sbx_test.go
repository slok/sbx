package lib_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/pkg/lib"
)

// newTestClient creates a client with a temp SQLite DB for test isolation.
func newTestClient(t *testing.T) *lib.Client {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	client, err := lib.New(ctx, lib.Config{
		DBPath:  dbPath,
		DataDir: t.TempDir(),
		Engine:  lib.EngineFake,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

func TestCreateSandbox(t *testing.T) {
	tests := map[string]struct {
		opts   lib.CreateSandboxOpts
		expErr bool
		expIs  error
	}{
		"Creating a sandbox with the fake engine should work.": {
			opts: lib.CreateSandboxOpts{
				Name:   "test-sandbox",
				Engine: lib.EngineFake,
				Resources: lib.Resources{
					VCPUs:    1,
					MemoryMB: 512,
					DiskGB:   5,
				},
			},
		},

		"Creating a sandbox with firecracker config should work.": {
			opts: lib.CreateSandboxOpts{
				Name:   "test-fc-sandbox",
				Engine: lib.EngineFake,
				Firecracker: &lib.FirecrackerConfig{
					RootFS:      "/fake/rootfs.ext4",
					KernelImage: "/fake/vmlinux",
				},
				Resources: lib.Resources{
					VCPUs:    2,
					MemoryMB: 1024,
					DiskGB:   10,
				},
			},
		},

		"Creating a sandbox without a name should fail.": {
			opts: lib.CreateSandboxOpts{
				Engine: lib.EngineFake,
				Resources: lib.Resources{
					VCPUs:    1,
					MemoryMB: 512,
					DiskGB:   5,
				},
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},

		"Creating a sandbox with an unsupported engine should fail.": {
			opts: lib.CreateSandboxOpts{
				Name:   "bad-engine",
				Engine: "docker",
				Resources: lib.Resources{
					VCPUs:    1,
					MemoryMB: 512,
					DiskGB:   5,
				},
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},

		"Creating a sandbox with zero resources should fail.": {
			opts: lib.CreateSandboxOpts{
				Name:   "zero-resources",
				Engine: lib.EngineFake,
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			ctx := context.Background()

			sb, err := client.CreateSandbox(ctx, test.opts)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.NotEmpty(sb.ID)
			assert.Equal(test.opts.Name, sb.Name)
			assert.Equal(lib.SandboxStatusCreated, sb.Status)
			assert.False(sb.CreatedAt.IsZero())
		})
	}
}

func TestCreateSandboxDuplicate(t *testing.T) {
	assert := assert.New(t)
	client := newTestClient(t)
	ctx := context.Background()

	opts := lib.CreateSandboxOpts{
		Name:   "dup-sandbox",
		Engine: lib.EngineFake,
		Resources: lib.Resources{
			VCPUs:    1,
			MemoryMB: 512,
			DiskGB:   5,
		},
	}

	_, err := client.CreateSandbox(ctx, opts)
	assert.NoError(err)

	_, err = client.CreateSandbox(ctx, opts)
	assert.Error(err)
	assert.True(errors.Is(err, lib.ErrAlreadyExists))
}

func TestGetSandbox(t *testing.T) {
	tests := map[string]struct {
		setup   func(t *testing.T, c *lib.Client) string // returns nameOrID to query
		expErr  bool
		expIs   error
		expName string
	}{
		"Getting a sandbox by name should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "by-name",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_ = sb
				return "by-name"
			},
			expName: "by-name",
		},

		"Getting a sandbox by ID should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "by-id",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.ID
			},
			expName: "by-id",
		},

		"Getting a non-existent sandbox should fail with not found.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "does-not-exist"
			},
			expErr: true,
			expIs:  lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			sb, err := client.GetSandbox(context.Background(), nameOrID)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.Equal(test.expName, sb.Name)
		})
	}
}

func TestListSandboxes(t *testing.T) {
	tests := map[string]struct {
		setup    func(t *testing.T, c *lib.Client)
		opts     *lib.ListSandboxesOpts
		expCount int
	}{
		"Listing with no sandboxes should return empty.": {
			setup:    func(t *testing.T, c *lib.Client) {},
			expCount: 0,
		},

		"Listing should return all sandboxes when no filter.": {
			setup: func(t *testing.T, c *lib.Client) {
				t.Helper()
				ctx := context.Background()
				for _, name := range []string{"a", "b", "c"} {
					_, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
						Name:      name,
						Engine:    lib.EngineFake,
						Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					})
					require.NoError(t, err)
				}
			},
			expCount: 3,
		},

		"Listing with status filter should filter correctly.": {
			setup: func(t *testing.T, c *lib.Client) {
				t.Helper()
				ctx := context.Background()
				// Create two sandboxes (both in "created" status).
				for _, name := range []string{"f1", "f2"} {
					_, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
						Name:      name,
						Engine:    lib.EngineFake,
						Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
					})
					require.NoError(t, err)
				}
			},
			opts: func() *lib.ListSandboxesOpts {
				s := lib.SandboxStatusRunning
				return &lib.ListSandboxesOpts{Status: &s}
			}(),
			expCount: 0, // None are running.
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			test.setup(t, client)

			sandboxes, err := client.ListSandboxes(context.Background(), test.opts)

			assert.NoError(err)
			assert.Len(sandboxes, test.expCount)
		})
	}
}

func TestStartSandbox(t *testing.T) {
	tests := map[string]struct {
		setup  func(t *testing.T, c *lib.Client) string
		opts   *lib.StartSandboxOpts
		expErr bool
		expIs  error
	}{
		"Starting a created sandbox should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "start-me",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
		},

		"Starting with session env should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "start-env",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
			opts: &lib.StartSandboxOpts{
				Env: map[string]string{"FOO": "bar"},
			},
		},

		"Starting a non-existent sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "ghost"
			},
			expErr: true,
			expIs:  lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			sb, err := client.StartSandbox(context.Background(), nameOrID, test.opts)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.Equal(lib.SandboxStatusRunning, sb.Status)
			assert.NotNil(sb.StartedAt)
		})
	}
}

func TestStopSandbox(t *testing.T) {
	tests := map[string]struct {
		setup  func(t *testing.T, c *lib.Client) string
		expErr bool
		expIs  error
	}{
		"Stopping a running sandbox should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "stop-me",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
		},

		"Stopping a created (not running) sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "not-running",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},

		"Stopping a non-existent sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "ghost"
			},
			expErr: true,
			expIs:  lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			sb, err := client.StopSandbox(context.Background(), nameOrID)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.Equal(lib.SandboxStatusStopped, sb.Status)
			assert.NotNil(sb.StoppedAt)
		})
	}
}

func TestRemoveSandbox(t *testing.T) {
	tests := map[string]struct {
		setup  func(t *testing.T, c *lib.Client) string
		force  bool
		expErr bool
		expIs  error
	}{
		"Removing a created sandbox should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "rm-created",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
		},

		"Removing a running sandbox without force should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "rm-running",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},

		"Removing a running sandbox with force should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "rm-force",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
			force: true,
		},

		"Removing a non-existent sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "ghost"
			},
			expErr: true,
			expIs:  lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			_, err := client.RemoveSandbox(context.Background(), nameOrID, test.force)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)

			// Verify sandbox is gone.
			_, err = client.GetSandbox(context.Background(), nameOrID)
			assert.True(errors.Is(err, lib.ErrNotFound))
		})
	}
}

func TestExec(t *testing.T) {
	tests := map[string]struct {
		setup   func(t *testing.T, c *lib.Client) string
		command []string
		opts    *lib.ExecOpts
		expErr  bool
		expIs   error
	}{
		"Executing in a running sandbox should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "exec-ok",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
			command: []string{"echo", "hello"},
		},

		"Executing with empty command should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "exec-empty",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
			command: []string{},
			expErr:  true,
			expIs:   lib.ErrNotValid,
		},

		"Executing in a non-running sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "exec-stopped",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
			command: []string{"echo", "hello"},
			expErr:  true,
			expIs:   lib.ErrNotValid,
		},

		"Executing in a non-existent sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "ghost"
			},
			command: []string{"echo", "hello"},
			expErr:  true,
			expIs:   lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			result, err := client.Exec(context.Background(), nameOrID, test.command, test.opts)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.Equal(0, result.ExitCode)
		})
	}
}

func TestCopyTo(t *testing.T) {
	t.Run("Copying to a running sandbox should work.", func(t *testing.T) {
		assert := assert.New(t)
		client := newTestClient(t)
		ctx := context.Background()

		sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
			Name:      "cp-to",
			Engine:    lib.EngineFake,
			Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
		})
		require.NoError(t, err)
		_, err = client.StartSandbox(ctx, sb.Name, nil)
		require.NoError(t, err)

		// Create a real temp file as source.
		srcPath := filepath.Join(t.TempDir(), "src.txt")
		require.NoError(t, os.WriteFile(srcPath, []byte("data"), 0644))

		err = client.CopyTo(ctx, sb.Name, srcPath, "/dst")
		assert.NoError(err)
	})

	t.Run("Copying to a non-running sandbox should fail.", func(t *testing.T) {
		assert := assert.New(t)
		client := newTestClient(t)

		sb, err := client.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
			Name:      "cp-to-stopped",
			Engine:    lib.EngineFake,
			Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
		})
		require.NoError(t, err)

		srcPath := filepath.Join(t.TempDir(), "src.txt")
		require.NoError(t, os.WriteFile(srcPath, []byte("data"), 0644))

		err = client.CopyTo(context.Background(), sb.Name, srcPath, "/dst")
		assert.Error(err)
		assert.True(errors.Is(err, lib.ErrNotValid), "expected ErrNotValid, got: %v", err)
	})
}

func TestCopyFrom(t *testing.T) {
	tests := map[string]struct {
		setup  func(t *testing.T, c *lib.Client) string
		expErr bool
		expIs  error
	}{
		"Copying from a running sandbox should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "cp-from",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
		},

		"Copying from a non-running sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "cp-from-stopped",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			err := client.CopyFrom(context.Background(), nameOrID, "/src", "/tmp/dst")

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
		})
	}
}

func TestFullLifecycle(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	client := newTestClient(t)
	ctx := context.Background()

	// Create.
	sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:   "lifecycle",
		Engine: lib.EngineFake,
		Resources: lib.Resources{
			VCPUs:    2,
			MemoryMB: 1024,
			DiskGB:   10,
		},
	})
	require.NoError(err)
	assert.Equal("lifecycle", sb.Name)
	assert.Equal(lib.SandboxStatusCreated, sb.Status)

	// List should have 1.
	sandboxes, err := client.ListSandboxes(ctx, nil)
	require.NoError(err)
	assert.Len(sandboxes, 1)

	// Get by name.
	got, err := client.GetSandbox(ctx, "lifecycle")
	require.NoError(err)
	assert.Equal(sb.ID, got.ID)

	// Get by ID.
	got, err = client.GetSandbox(ctx, sb.ID)
	require.NoError(err)
	assert.Equal("lifecycle", got.Name)

	// Start.
	started, err := client.StartSandbox(ctx, "lifecycle", nil)
	require.NoError(err)
	assert.Equal(lib.SandboxStatusRunning, started.Status)
	assert.NotNil(started.StartedAt)

	// Exec.
	result, err := client.Exec(ctx, "lifecycle", []string{"echo", "hello"}, nil)
	require.NoError(err)
	assert.Equal(0, result.ExitCode)

	// CopyTo.
	srcPath := filepath.Join(t.TempDir(), "src.txt")
	require.NoError(os.WriteFile(srcPath, []byte("data"), 0644))
	err = client.CopyTo(ctx, "lifecycle", srcPath, "/dst")
	require.NoError(err)

	// CopyFrom.
	err = client.CopyFrom(ctx, "lifecycle", "/src", "/tmp/dst")
	require.NoError(err)

	// Stop.
	stopped, err := client.StopSandbox(ctx, "lifecycle")
	require.NoError(err)
	assert.Equal(lib.SandboxStatusStopped, stopped.Status)
	assert.NotNil(stopped.StoppedAt)

	// Remove.
	_, err = client.RemoveSandbox(ctx, "lifecycle", false)
	require.NoError(err)

	// Verify gone.
	_, err = client.GetSandbox(ctx, "lifecycle")
	assert.True(errors.Is(err, lib.ErrNotFound))

	// List should be empty.
	sandboxes, err = client.ListSandboxes(ctx, nil)
	require.NoError(err)
	assert.Len(sandboxes, 0)
}

func TestCopyToSourceValidation(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	client := newTestClient(t)
	ctx := context.Background()

	// Create and start a sandbox.
	_, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      "cp-validation",
		Engine:    lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	require.NoError(err)
	_, err = client.StartSandbox(ctx, "cp-validation", nil)
	require.NoError(err)

	// CopyTo with non-existent source should fail with ErrNotValid.
	err = client.CopyTo(ctx, "cp-validation", "/nonexistent/path/file.txt", "/dst")
	assert.Error(err)
	assert.True(errors.Is(err, lib.ErrNotValid), "expected ErrNotValid, got: %v", err)
}

func TestCreateSnapshot(t *testing.T) {
	tests := map[string]struct {
		setup  func(t *testing.T, c *lib.Client) string
		opts   *lib.CreateSnapshotOpts
		expErr bool
		expIs  error
	}{
		"Creating a snapshot of a created sandbox should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "snap-created",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
		},

		"Creating a snapshot with a custom name should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				sb, err := c.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
					Name:      "snap-named",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				return sb.Name
			},
			opts: &lib.CreateSnapshotOpts{SnapshotName: "my-snapshot"},
		},

		"Creating a snapshot of a running sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				sb, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "snap-running",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				_, err = c.StartSandbox(ctx, sb.Name, nil)
				require.NoError(t, err)
				return sb.Name
			},
			expErr: true,
			expIs:  lib.ErrNotValid,
		},

		"Creating a snapshot of a non-existent sandbox should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "ghost"
			},
			expErr: true,
			expIs:  lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			snap, err := client.CreateSnapshot(context.Background(), nameOrID, test.opts)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.NotEmpty(snap.ID)
			assert.NotEmpty(snap.Name)
			assert.False(snap.CreatedAt.IsZero())
		})
	}
}

func TestListSnapshots(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	client := newTestClient(t)
	ctx := context.Background()

	// Initially empty.
	snaps, err := client.ListSnapshots(ctx)
	require.NoError(err)
	assert.Len(snaps, 0)

	// Create sandbox + snapshot.
	_, err = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      "list-snap",
		Engine:    lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	require.NoError(err)

	_, err = client.CreateSnapshot(ctx, "list-snap", &lib.CreateSnapshotOpts{SnapshotName: "snap-1"})
	require.NoError(err)

	_, err = client.CreateSnapshot(ctx, "list-snap", &lib.CreateSnapshotOpts{SnapshotName: "snap-2"})
	require.NoError(err)

	// Should have 2.
	snaps, err = client.ListSnapshots(ctx)
	require.NoError(err)
	assert.Len(snaps, 2)
}

func TestRemoveSnapshot(t *testing.T) {
	tests := map[string]struct {
		setup  func(t *testing.T, c *lib.Client) string // returns snapshot nameOrID to remove
		expErr bool
		expIs  error
	}{
		"Removing a snapshot by name should work.": {
			setup: func(t *testing.T, c *lib.Client) string {
				t.Helper()
				ctx := context.Background()
				_, err := c.CreateSandbox(ctx, lib.CreateSandboxOpts{
					Name:      "rm-snap",
					Engine:    lib.EngineFake,
					Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
				})
				require.NoError(t, err)
				snap, err := c.CreateSnapshot(ctx, "rm-snap", &lib.CreateSnapshotOpts{SnapshotName: "to-remove"})
				require.NoError(t, err)
				return snap.Name
			},
		},

		"Removing a non-existent snapshot should fail.": {
			setup: func(t *testing.T, c *lib.Client) string {
				return "ghost-snapshot"
			},
			expErr: true,
			expIs:  lib.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			client := newTestClient(t)
			nameOrID := test.setup(t, client)

			snap, err := client.RemoveSnapshot(context.Background(), nameOrID)

			if test.expErr {
				assert.Error(err)
				if test.expIs != nil {
					assert.True(errors.Is(err, test.expIs), "expected error %v, got: %v", test.expIs, err)
				}
				return
			}

			assert.NoError(err)
			assert.NotEmpty(snap.ID)

			// Verify snapshot is gone.
			snaps, err := client.ListSnapshots(context.Background())
			assert.NoError(err)
			assert.Len(snaps, 0)
		})
	}
}

func TestSnapshotLifecycle(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	client := newTestClient(t)
	ctx := context.Background()

	// Create a sandbox.
	_, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      "snap-lifecycle",
		Engine:    lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	require.NoError(err)

	// Create snapshot.
	snap, err := client.CreateSnapshot(ctx, "snap-lifecycle", &lib.CreateSnapshotOpts{
		SnapshotName: "my-snap",
	})
	require.NoError(err)
	assert.Equal("my-snap", snap.Name)
	assert.NotEmpty(snap.ID)

	// List should have 1.
	snaps, err := client.ListSnapshots(ctx)
	require.NoError(err)
	assert.Len(snaps, 1)
	assert.Equal("my-snap", snaps[0].Name)

	// Remove.
	removed, err := client.RemoveSnapshot(ctx, "my-snap")
	require.NoError(err)
	assert.Equal(snap.ID, removed.ID)

	// List should be empty.
	snaps, err = client.ListSnapshots(ctx)
	require.NoError(err)
	assert.Len(snaps, 0)
}

func TestForward(t *testing.T) {
	t.Run("Forwarding with empty ports should fail.", func(t *testing.T) {
		assert := assert.New(t)
		client := newTestClient(t)
		ctx := context.Background()

		sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
			Name:      "fwd-empty",
			Engine:    lib.EngineFake,
			Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
		})
		require.NoError(t, err)
		_, err = client.StartSandbox(ctx, sb.Name, nil)
		require.NoError(t, err)

		err = client.Forward(ctx, sb.Name, []lib.PortMapping{})
		assert.Error(err)
		assert.True(errors.Is(err, lib.ErrNotValid), "expected ErrNotValid, got: %v", err)
	})

	t.Run("Forwarding to a non-existent sandbox should fail.", func(t *testing.T) {
		assert := assert.New(t)
		client := newTestClient(t)

		err := client.Forward(context.Background(), "ghost", []lib.PortMapping{{LocalPort: 8080, RemotePort: 8080}})
		assert.Error(err)
		assert.True(errors.Is(err, lib.ErrNotFound), "expected ErrNotFound, got: %v", err)
	})

	t.Run("Forwarding to a non-running sandbox should fail.", func(t *testing.T) {
		assert := assert.New(t)
		client := newTestClient(t)

		_, err := client.CreateSandbox(context.Background(), lib.CreateSandboxOpts{
			Name:      "fwd-stopped",
			Engine:    lib.EngineFake,
			Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
		})
		require.NoError(t, err)

		err = client.Forward(context.Background(), "fwd-stopped", []lib.PortMapping{{LocalPort: 8080, RemotePort: 8080}})
		assert.Error(err)
		assert.True(errors.Is(err, lib.ErrNotValid), "expected ErrNotValid, got: %v", err)
	})
}

func TestDoctor(t *testing.T) {
	assert := assert.New(t)
	client := newTestClient(t) // Uses EngineFake.
	ctx := context.Background()

	// Doctor with fake engine should return empty results.
	results, err := client.Doctor(ctx)
	assert.NoError(err)
	assert.NotNil(results)
	assert.Len(results, 0)
}

func TestResourcesPreserved(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	client := newTestClient(t)
	ctx := context.Background()

	sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:   "resources",
		Engine: lib.EngineFake,
		Resources: lib.Resources{
			VCPUs:    1.5,
			MemoryMB: 2048,
			DiskGB:   20,
		},
	})
	require.NoError(err)

	got, err := client.GetSandbox(ctx, sb.Name)
	require.NoError(err)

	assert.Equal(1.5, got.Config.Resources.VCPUs)
	assert.Equal(2048, got.Config.Resources.MemoryMB)
	assert.Equal(20, got.Config.Resources.DiskGB)
}
