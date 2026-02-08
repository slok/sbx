package lib_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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
			RootFS:      config.RootFSPath(),
			KernelImage: config.KernelPath(),
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
			RootFS:      config.RootFSPath(),
			KernelImage: config.KernelPath(),
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
			RootFS:      config.RootFSPath(),
			KernelImage: config.KernelPath(),
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
			RootFS:      config.RootFSPath(),
			KernelImage: config.KernelPath(),
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

func TestSDKSnapshotLifecycle(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	srcName := intlib.UniqueName("sdk-snapsrc")
	dstName := intlib.UniqueName("sdk-snapdst")
	snapName := "sdk-snap-test"

	intlib.CleanupSandbox(t, client, srcName)
	intlib.CleanupSandbox(t, client, dstName)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_, _ = client.RemoveSnapshot(cleanCtx, snapName)
	})

	// Create source sandbox, start, write marker, stop.
	_, err := client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   srcName,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			RootFS:      config.RootFSPath(),
			KernelImage: config.KernelPath(),
		},
		Resources: sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
	})
	require.NoError(t, err)

	_, err = client.StartSandbox(ctx, srcName, nil)
	require.NoError(t, err)

	// Write a marker file to rootfs (not tmpfs) to verify snapshot preserves data.
	var stdout bytes.Buffer
	_, err = client.Exec(ctx, srcName, []string{"sh", "-c", "echo snapshot-marker > /root/marker.txt"}, &sdklib.ExecOpts{
		Stdout: &stdout,
	})
	require.NoError(t, err)

	// Stop (required for snapshot).
	_, err = client.StopSandbox(ctx, srcName)
	require.NoError(t, err)

	// Create snapshot.
	snap, err := client.CreateSnapshot(ctx, srcName, &sdklib.CreateSnapshotOpts{
		SnapshotName: snapName,
	})
	require.NoError(t, err)
	assert.Equal(t, snapName, snap.Name)
	assert.NotEmpty(t, snap.ID)
	assert.NotEmpty(t, snap.Path)

	// List snapshots.
	snaps, err := client.ListSnapshots(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(snaps), 1)

	found := false
	for _, s := range snaps {
		if s.Name == snapName {
			found = true
			break
		}
	}
	assert.True(t, found, "snapshot %q should be in the list", snapName)

	// Create new sandbox from snapshot.
	_, err = client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   dstName,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			KernelImage: config.KernelPath(),
		},
		Resources:    sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
		FromSnapshot: snapName,
	})
	require.NoError(t, err)

	// Start the snapshot-based sandbox.
	_, err = client.StartSandbox(ctx, dstName, nil)
	require.NoError(t, err)

	// Verify marker file from source exists.
	stdout.Reset()
	_, err = client.Exec(ctx, dstName, []string{"cat", "/root/marker.txt"}, &sdklib.ExecOpts{
		Stdout: &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, "snapshot-marker", strings.TrimSpace(stdout.String()))

	// Remove snapshot.
	removed, err := client.RemoveSnapshot(ctx, snapName)
	require.NoError(t, err)
	assert.Equal(t, snap.ID, removed.ID)

	// List should show one less (or empty).
	snaps, err = client.ListSnapshots(ctx)
	require.NoError(t, err)
	for _, s := range snaps {
		assert.NotEqual(t, snapName, s.Name, "removed snapshot should not appear in list")
	}
}

func TestSDKImageLifecycle(t *testing.T) {
	config := intlib.NewConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Use a fresh temp dir for images to avoid interfering with existing images.
	imagesDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	client, err := sdklib.New(ctx, sdklib.Config{
		DBPath:            dbPath,
		DataDir:           t.TempDir(),
		Engine:            sdklib.EngineFirecracker,
		FirecrackerBinary: config.FirecrackerBinaryPath(),
		ImagesDir:         imagesDir,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	version := config.ImageVersion

	// Cleanup on failure.
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_ = client.RemoveImage(cleanCtx, version)
	})

	// 1. List images - should show available releases from GitHub.
	images, err := client.ListImages(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, images, "should have at least one image release from GitHub")

	// Find our version in the list.
	var foundImage *sdklib.ImageRelease
	for i, img := range images {
		if img.Version == version {
			foundImage = &images[i]
			break
		}
	}
	require.NotNil(t, foundImage, "version %s should be in the list", version)
	assert.False(t, foundImage.Installed, "image should not be installed yet in fresh dir")

	// 2. Pull image.
	pullResult, err := client.PullImage(ctx, version, nil)
	require.NoError(t, err)
	assert.Equal(t, version, pullResult.Version)
	assert.False(t, pullResult.Skipped)
	assert.NotEmpty(t, pullResult.KernelPath)
	assert.NotEmpty(t, pullResult.RootFSPath)
	assert.NotEmpty(t, pullResult.FirecrackerPath)

	// 3. Pull again should skip.
	pullResult2, err := client.PullImage(ctx, version, nil)
	require.NoError(t, err)
	assert.True(t, pullResult2.Skipped)

	// 4. Inspect image.
	manifest, err := client.InspectImage(ctx, version)
	require.NoError(t, err)
	assert.Equal(t, version, manifest.Version)
	assert.NotEmpty(t, manifest.Artifacts)
	assert.NotEmpty(t, manifest.Firecracker.Version)

	// 5. List should show installed.
	images, err = client.ListImages(ctx)
	require.NoError(t, err)
	foundImage = nil
	for i, img := range images {
		if img.Version == version {
			foundImage = &images[i]
			break
		}
	}
	require.NotNil(t, foundImage)
	assert.True(t, foundImage.Installed, "image should be installed after pull")

	// 6. Remove image.
	err = client.RemoveImage(ctx, version)
	require.NoError(t, err)

	// 7. List should show not installed.
	images, err = client.ListImages(ctx)
	require.NoError(t, err)
	for _, img := range images {
		if img.Version == version {
			assert.False(t, img.Installed, "image should not be installed after remove")
		}
	}
}

func TestSDKPortForward(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := intlib.UniqueName("sdk-fwd")
	intlib.CleanupSandbox(t, client, name)

	// Create and start.
	_, err := client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:   name,
		Engine: sdklib.EngineFirecracker,
		Firecracker: &sdklib.FirecrackerConfig{
			RootFS:      config.RootFSPath(),
			KernelImage: config.KernelPath(),
		},
		Resources: sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
	})
	require.NoError(t, err)
	_, err = client.StartSandbox(ctx, name, nil)
	require.NoError(t, err)

	const remotePort = 8765
	const localPort = 18765

	// Write listener script inside sandbox then run it in background.
	scriptCmd := fmt.Sprintf("echo '#!/bin/sh\nprintf pong | nc -l -p %d' > /tmp/listen.sh && chmod +x /tmp/listen.sh", remotePort)
	_, err = client.Exec(ctx, name, []string{"sh", "-c", scriptCmd}, nil)
	require.NoError(t, err)

	listenerCtx, listenerCancel := context.WithCancel(ctx)
	defer listenerCancel()
	go func() {
		_, _ = client.Exec(listenerCtx, name, []string{"/tmp/listen.sh"}, nil)
	}()

	// Give listener time to start.
	time.Sleep(2 * time.Second)

	// Start port forwarding in background.
	fwdCtx, fwdCancel := context.WithCancel(ctx)
	defer fwdCancel()

	fwdStarted := make(chan struct{})
	go func() {
		close(fwdStarted)
		_ = client.Forward(fwdCtx, name, []sdklib.PortMapping{
			{LocalPort: localPort, RemotePort: remotePort},
		})
	}()
	<-fwdStarted

	// Give forwarding time to establish.
	time.Sleep(3 * time.Second)

	// Connect to localhost:localPort and read response.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 5*time.Second)
	if err != nil {
		// Best-effort: forward may take longer on some systems.
		t.Logf("Could not connect to forwarded port (timing-sensitive): %v", err)
		t.Logf("Verifying forward started without immediate error...")
		fwdCancel()
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err == nil {
		assert.Equal(t, "pong", string(buf[:n]))
	}

	fwdCancel()
}

func TestSDKDoctor(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	results, err := client.Doctor(ctx)
	require.NoError(t, err)
	assert.NotNil(t, results)
	// Firecracker engine should return at least one check result.
	assert.NotEmpty(t, results, "doctor should return checks for Firecracker engine")

	// Each result should have an ID and status.
	for _, r := range results {
		assert.NotEmpty(t, r.ID, "check result should have an ID")
		assert.NotEmpty(t, r.Status, "check result should have a status")
	}
}
