package lib_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdklib "github.com/slok/sbx/pkg/lib"
	intlib "github.com/slok/sbx/test/integration/lib"
)

func TestSDKSandboxLifecycle(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := intlib.UniqueName("sdk-lifecycle")
	intlib.CleanupSandbox(t, client, name)

	// Create.
	sb, err := client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   name,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			RootFS:            config.RootFSPath(),
			KernelImage:       config.KernelPath(),
			FirecrackerBinary: config.FirecrackerBinaryPath(),
		},
		Resources: sdklib.Resources{
			VCPUs:    1,
			MemoryMB: 512,
			DiskGB:   2,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, name, sb.Name)
	assert.NotEmpty(t, sb.ID)

	// List should have 1.
	sandboxes, err := client.ListSandboxes(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, sandboxes, 1)
	assert.Equal(t, name, sandboxes[0].Name)

	// Start.
	started, err := client.StartSandbox(ctx, name, nil)
	require.NoError(t, err)
	assert.Equal(t, sdklib.SandboxStatusRunning, started.Status)

	// Get should show running.
	got, err := client.GetSandbox(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, sdklib.SandboxStatusRunning, got.Status)

	// Stop.
	stopped, err := client.StopSandbox(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, sdklib.SandboxStatusStopped, stopped.Status)

	// Get should show stopped.
	got, err = client.GetSandbox(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, sdklib.SandboxStatusStopped, got.Status)

	// Remove.
	_, err = client.RemoveSandbox(ctx, name, false)
	require.NoError(t, err)

	// List should be empty.
	sandboxes, err = client.ListSandboxes(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, sandboxes, 0)
}

func TestSDKExec(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := intlib.UniqueName("sdk-exec")
	intlib.CleanupSandbox(t, client, name)

	// Create and start.
	_, err := client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   name,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			RootFS:            config.RootFSPath(),
			KernelImage:       config.KernelPath(),
			FirecrackerBinary: config.FirecrackerBinaryPath(),
		},
		Resources: sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
	})
	require.NoError(t, err)
	_, err = client.StartSandbox(ctx, name, nil)
	require.NoError(t, err)

	// Exec echo.
	var stdout bytes.Buffer
	result, err := client.Exec(ctx, name, []string{"echo", "hello-sdk"}, &sdklib.ExecOpts{
		Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "hello-sdk")

	// Exec with env.
	stdout.Reset()
	result, err = client.Exec(ctx, name, []string{"sh", "-c", "echo $TEST_VAR"}, &sdklib.ExecOpts{
		Stdout: &stdout,
		Env:    map[string]string{"TEST_VAR": "sdk-value"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "sdk-value")

	// Exec failing command.
	result, err = client.Exec(ctx, name, []string{"sh", "-c", "exit 42"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)

	// Exec in non-existent sandbox.
	_, err = client.Exec(ctx, "does-not-exist", []string{"echo"}, nil)
	assert.True(t, errors.Is(err, sdklib.ErrNotFound))
}

func TestSDKCopy(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := intlib.UniqueName("sdk-copy")
	intlib.CleanupSandbox(t, client, name)

	// Create and start.
	_, err := client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   name,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			RootFS:            config.RootFSPath(),
			KernelImage:       config.KernelPath(),
			FirecrackerBinary: config.FirecrackerBinaryPath(),
		},
		Resources: sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
	})
	require.NoError(t, err)
	_, err = client.StartSandbox(ctx, name, nil)
	require.NoError(t, err)

	// Write a temp file on host.
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "sdk-test.txt")
	require.NoError(t, os.WriteFile(srcPath, []byte("sdk-copy-test"), 0644))

	// CopyTo sandbox.
	err = client.CopyTo(ctx, name, srcPath, "/tmp/sdk-test.txt")
	require.NoError(t, err)

	// Verify inside sandbox.
	var stdout bytes.Buffer
	_, err = client.Exec(ctx, name, []string{"cat", "/tmp/sdk-test.txt"}, &sdklib.ExecOpts{
		Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, "sdk-copy-test", stdout.String())

	// CopyFrom sandbox.
	dstPath := filepath.Join(tmpDir, "sdk-test-from.txt")
	err = client.CopyFrom(ctx, name, "/tmp/sdk-test.txt", dstPath)
	require.NoError(t, err)

	// Verify on host.
	data, err := os.ReadFile(dstPath)
	require.NoError(t, err)
	assert.Equal(t, "sdk-copy-test", string(data))
}

func TestSDKStartWithEnv(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := intlib.UniqueName("sdk-env")
	intlib.CleanupSandbox(t, client, name)

	// Create.
	_, err := client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   name,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			RootFS:            config.RootFSPath(),
			KernelImage:       config.KernelPath(),
			FirecrackerBinary: config.FirecrackerBinaryPath(),
		},
		Resources: sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
	})
	require.NoError(t, err)

	// Start with session env.
	_, err = client.StartSandbox(ctx, name, &sdklib.StartSandboxOpts{
		Env: map[string]string{
			"MY_VAR": "hello-from-sdk",
		},
	})
	require.NoError(t, err)

	// Verify env via exec.
	var stdout bytes.Buffer
	_, err = client.Exec(ctx, name, []string{"sh", "-c", ". /etc/sbx/session-env.sh && echo $MY_VAR"}, &sdklib.ExecOpts{
		Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "hello-from-sdk")
}
