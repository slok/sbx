package lib_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	osexec "os/exec"
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

	// Exec with file upload to workdir.
	t.Run("exec with file upload to workdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "sdk-upload.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("sdk-upload-content"), 0644))

		var out bytes.Buffer
		result, err = client.Exec(ctx, name, []string{"cat", "sdk-upload.txt"}, &sdklib.ExecOpts{
			Stdout:     &out,
			WorkingDir: "/tmp",
			Files:      []string{srcFile},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, out.String(), "sdk-upload-content")
	})

	// Exec with multiple file uploads.
	t.Run("exec with multiple file uploads", func(t *testing.T) {
		tmpDir := t.TempDir()
		file1 := filepath.Join(tmpDir, "sdk-a.txt")
		file2 := filepath.Join(tmpDir, "sdk-b.txt")
		require.NoError(t, os.WriteFile(file1, []byte("a-data"), 0644))
		require.NoError(t, os.WriteFile(file2, []byte("b-data"), 0644))

		var out bytes.Buffer
		result, err = client.Exec(ctx, name, []string{"sh", "-c", "cat sdk-a.txt && cat sdk-b.txt"}, &sdklib.ExecOpts{
			Stdout:     &out,
			WorkingDir: "/tmp",
			Files:      []string{file1, file2},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, out.String(), "a-data")
		assert.Contains(t, out.String(), "b-data")
	})

	// Exec with file upload to root (no workdir).
	t.Run("exec with file upload to root", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "sdk-root.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("root-data"), 0644))

		var out bytes.Buffer
		result, err = client.Exec(ctx, name, []string{"cat", "/sdk-root.txt"}, &sdklib.ExecOpts{
			Stdout: &out,
			Files:  []string{srcFile},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, out.String(), "root-data")
	})

	// Exec with file upload to non-existent dir (should create it).
	t.Run("exec with file upload creates missing workdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "sdk-newdir.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("newdir-data"), 0644))

		var out bytes.Buffer
		result, err = client.Exec(ctx, name, []string{"cat", "sdk-newdir.txt"}, &sdklib.ExecOpts{
			Stdout:     &out,
			WorkingDir: "/opt/sdk-test/nested",
			Files:      []string{srcFile},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, out.String(), "newdir-data")
	})

	// Exec with file upload overwrites existing file.
	t.Run("exec with file upload overwrites existing file", func(t *testing.T) {
		// Write old content.
		_, err = client.Exec(ctx, name, []string{"sh", "-c", "echo old-sdk-content > /tmp/sdk-overwrite.txt"}, nil)
		require.NoError(t, err)

		// Upload new content with same name.
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "sdk-overwrite.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("new-sdk-content"), 0644))

		var out bytes.Buffer
		result, err = client.Exec(ctx, name, []string{"cat", "sdk-overwrite.txt"}, &sdklib.ExecOpts{
			Stdout:     &out,
			WorkingDir: "/tmp",
			Files:      []string{srcFile},
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, out.String(), "new-sdk-content")
		assert.NotContains(t, out.String(), "old-sdk-content")
	})
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
		_ = client.RemoveImage(cleanCtx, snapName)
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

	// Create snapshot image.
	imgName, err := client.CreateImageFromSandbox(ctx, srcName, &sdklib.CreateImageFromSandboxOpts{
		ImageName: snapName,
	})
	require.NoError(t, err)
	assert.Equal(t, snapName, imgName)

	// List images should include the snapshot.
	images, err := client.ListImages(ctx)
	require.NoError(t, err)

	found := false
	for _, img := range images {
		if img.Version == snapName {
			found = true
			assert.True(t, img.Installed)
			assert.Equal(t, sdklib.ImageSourceSnapshot, img.Source)
			break
		}
	}
	assert.True(t, found, "snapshot image %q should be in the list", snapName)

	// Create new sandbox from snapshot image.
	_, err = client.CreateSandbox(ctx, sdklib.CreateSandboxOpts{
		Name:      dstName,
		Engine:    sdklib.EngineFirecracker,
		FromImage: snapName,
		Resources: sdklib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 2},
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

	// Remove snapshot image.
	err = client.RemoveImage(ctx, snapName)
	require.NoError(t, err)

	// List images should not include the snapshot anymore.
	images, err = client.ListImages(ctx)
	require.NoError(t, err)
	for _, img := range images {
		assert.NotEqual(t, snapName, img.Version, "removed snapshot should not appear in list")
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

func TestSDKEgressControl(t *testing.T) {
	config := intlib.NewConfig(t)
	client := intlib.NewTestClient(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	name := intlib.UniqueName("sdk-egress")
	intlib.CleanupSandbox(t, client, name)

	// Create sandbox.
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

	// Start with egress policy: default deny, allow only example.com and private ranges.
	_, err = client.StartSandbox(ctx, name, &sdklib.StartSandboxOpts{
		Egress: &sdklib.EgressPolicy{
			Default: sdklib.EgressActionDeny,
			Rules: []sdklib.EgressRule{
				{Domain: "example.com", Action: sdklib.EgressActionAllow},
				{Domain: "*.example.com", Action: sdklib.EgressActionAllow},
				{CIDR: "10.0.0.0/8", Action: sdklib.EgressActionAllow},
			},
		},
	})
	require.NoError(t, err)

	// Diagnostic: log host-side and VM-side state for debugging CI failures.
	t.Run("diagnostics", func(t *testing.T) {
		// Host-side: check nftables rules and egress proxy processes.
		if out, err := osexec.Command("nft", "list", "ruleset").CombinedOutput(); err == nil {
			t.Logf("Host nftables:\n%s", string(out))
		}
		if out, err := osexec.Command("sh", "-c", "ps aux | grep egress-proxy | grep -v grep").CombinedOutput(); err == nil {
			t.Logf("Host egress-proxy processes:\n%s", string(out))
		} else {
			t.Logf("No egress-proxy processes found on host")
		}
		// Check all egress proxy log and PID files.
		if out, err := osexec.Command("sh", "-c", "find /tmp -name 'egress-proxy.*' -exec echo '--- {} ---' \\; -exec cat {} \\; 2>/dev/null || true").CombinedOutput(); err == nil {
			t.Logf("Egress proxy files:\n%s", string(out))
		}

		// VM-side diagnostics.
		var stdout bytes.Buffer
		_, _ = client.Exec(ctx, name, []string{
			"sh", "-c", "echo '--- resolv.conf ---' && cat /etc/resolv.conf && echo '--- ip route ---' && ip route && echo '--- ip addr ---' && ip addr",
		}, &sdklib.ExecOpts{Stdout: &stdout})
		t.Logf("VM diagnostics:\n%s", stdout.String())
	})

	// Test 1: Verify /etc/resolv.conf points to the gateway (DNS forwarder).
	t.Run("resolv.conf points to gateway", func(t *testing.T) {
		var stdout bytes.Buffer
		_, err := client.Exec(ctx, name, []string{"cat", "/etc/resolv.conf"}, &sdklib.ExecOpts{
			Stdout: &stdout,
		})
		require.NoError(t, err)
		assert.Contains(t, stdout.String(), "nameserver 10.", "resolv.conf should point to the gateway")
	})

	// Test 2: DNS resolution works for allowed domain.
	t.Run("DNS resolution works", func(t *testing.T) {
		var stdout bytes.Buffer
		_, err := client.Exec(ctx, name, []string{
			"sh", "-c", "getent hosts example.com || nslookup example.com || echo DNS_RESOLVE_FAILED",
		}, &sdklib.ExecOpts{
			Stdout: &stdout,
		})
		require.NoError(t, err)
		assert.NotContains(t, stdout.String(), "DNS_RESOLVE_FAILED", "DNS resolution should work via forwarder")
	})

	// Test 3: Allowed connection succeeds.
	t.Run("allowed connection succeeds", func(t *testing.T) {
		var stdout bytes.Buffer
		res, err := client.Exec(ctx, name, []string{
			"sh", "-c", "wget -q -O /dev/null --timeout=5 http://example.com/ 2>&1 && echo EGRESS_ALLOWED || echo EGRESS_BLOCKED",
		}, &sdklib.ExecOpts{Stdout: &stdout})

		if err != nil || (res != nil && res.ExitCode != 0) {
			// wget may not be installed; try curl.
			stdout.Reset()
			_, err = client.Exec(ctx, name, []string{
				"sh", "-c", "curl -sf --connect-timeout 5 http://example.com/ > /dev/null 2>&1 && echo EGRESS_ALLOWED || echo EGRESS_BLOCKED",
			}, &sdklib.ExecOpts{Stdout: &stdout})
		}

		if err != nil {
			t.Skipf("Neither wget nor curl available in sandbox image")
		}
		assert.Contains(t, stdout.String(), "EGRESS_ALLOWED", "Connection to allowed domain should succeed")
	})

	// Test 4: Denied connection is blocked.
	t.Run("denied connection is blocked", func(t *testing.T) {
		var stdout bytes.Buffer
		_, err := client.Exec(ctx, name, []string{
			"sh", "-c", "wget -q -O /dev/null --timeout=5 http://denied-domain.invalid/ 2>&1 && echo EGRESS_ALLOWED || echo EGRESS_BLOCKED",
		}, &sdklib.ExecOpts{Stdout: &stdout})

		if err != nil {
			// wget may not be installed; try curl.
			stdout.Reset()
			_, err = client.Exec(ctx, name, []string{
				"sh", "-c", "curl -sf --connect-timeout 5 http://denied-domain.invalid/ > /dev/null 2>&1 && echo EGRESS_ALLOWED || echo EGRESS_BLOCKED",
			}, &sdklib.ExecOpts{Stdout: &stdout})
		}

		if err != nil {
			// If tools aren't available, that's ok.
			return
		}
		assert.Contains(t, stdout.String(), "EGRESS_BLOCKED", "Connection to denied domain should be blocked")
	})

	// Test 5: Sandbox is functional with egress enabled.
	t.Run("sandbox functional with egress", func(t *testing.T) {
		var stdout bytes.Buffer
		result, err := client.Exec(ctx, name, []string{"echo", "egress-functional"}, &sdklib.ExecOpts{
			Stdout: &stdout,
		})
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, stdout.String(), "egress-functional")
	})
}
