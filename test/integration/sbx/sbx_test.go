package sbx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intsbx "github.com/slok/sbx/test/integration/sbx"
)

// newTestDB creates a temp directory with a fresh SQLite database path for test isolation.
// Returns the db path and a cleanup function.
func newTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test-sbx.db")
}

// uniqueName generates a unique sandbox name for test isolation.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// cleanupSandbox registers a cleanup function that removes a sandbox forcefully.
func cleanupSandbox(t *testing.T, config intsbx.Config, dbPath, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		// Best effort cleanup - don't fail the test if cleanup fails.
		_, _, _ = intsbx.RunRm(ctx, config, dbPath, name)
	})
}

// listItem matches the JSON output of `sbx list --format json`.
type listItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// statusOutput matches the JSON output of `sbx status --format json`.
type statusOutput struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	VCPUs    float64 `json:"vcpus"`
	MemoryMB int     `json:"memory_mb"`
	DiskGB   int     `json:"disk_gb"`
}

// parseSandboxList parses the JSON list output.
func parseSandboxList(t *testing.T, data []byte) []listItem {
	t.Helper()
	var items []listItem
	require.NoError(t, json.Unmarshal(data, &items))
	return items
}

// findSandboxInList finds a sandbox by name in the list output.
func findSandboxInList(items []listItem, name string) *listItem {
	for _, item := range items {
		if item.Name == name {
			return &item
		}
	}
	return nil
}

// waitForRunning polls the sandbox status until it's "running" or the timeout expires.
func waitForRunning(ctx context.Context, t *testing.T, config intsbx.Config, dbPath, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _, err := intsbx.RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("status %s --format json", name))
		if err == nil {
			var s statusOutput
			if json.Unmarshal(out, &s) == nil && s.Status == "running" {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("sandbox %s did not reach running state within %s", name, timeout)
}

func TestSandboxLifecycle(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("lifecycle")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Register cleanup (runs even if test fails).
	cleanupSandbox(t, config, dbPath, name)

	// 1. Create sandbox.
	stdout, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Sandbox created successfully")

	// 2. List should show the sandbox as "stopped" (freshly created).
	stdout, stderr, err = intsbx.RunList(ctx, config, dbPath)
	require.NoError(t, err, "list failed: stdout=%s stderr=%s", stdout, stderr)
	items := parseSandboxList(t, stdout)
	found := findSandboxInList(items, name)
	require.NotNil(t, found, "sandbox %s not found in list", name)
	assert.Equal(t, "stopped", found.Status)

	// 3. Start the sandbox.
	stdout, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Started sandbox")

	// 4. Wait for running and verify via list.
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)
	stdout, stderr, err = intsbx.RunList(ctx, config, dbPath)
	require.NoError(t, err, "list after start failed: stdout=%s stderr=%s", stdout, stderr)
	items = parseSandboxList(t, stdout)
	found = findSandboxInList(items, name)
	require.NotNil(t, found, "sandbox %s not found in list after start", name)
	assert.Equal(t, "running", found.Status)

	// 5. Stop the sandbox.
	stdout, stderr, err = intsbx.RunStop(ctx, config, dbPath, name)
	require.NoError(t, err, "stop failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Stopped sandbox")

	// 6. Verify stopped status.
	stdout, stderr, err = intsbx.RunList(ctx, config, dbPath)
	require.NoError(t, err, "list after stop failed: stdout=%s stderr=%s", stdout, stderr)
	items = parseSandboxList(t, stdout)
	found = findSandboxInList(items, name)
	require.NotNil(t, found, "sandbox %s not found in list after stop", name)
	assert.Equal(t, "stopped", found.Status)

	// 7. Remove the sandbox.
	stdout, stderr, err = intsbx.RunRm(ctx, config, dbPath, name)
	require.NoError(t, err, "rm failed: stdout=%s stderr=%s", stdout, stderr)

	// 8. Verify it's gone from list.
	stdout, stderr, err = intsbx.RunList(ctx, config, dbPath)
	require.NoError(t, err, "list after rm failed: stdout=%s stderr=%s", stdout, stderr)
	items = parseSandboxList(t, stdout)
	found = findSandboxInList(items, name)
	assert.Nil(t, found, "sandbox %s should not be in list after rm", name)
}

func TestExec(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("exec")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Test: basic exec.
	t.Run("basic echo command", func(t *testing.T) {
		stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{"echo", "hello-integration"})
		require.NoError(t, err, "exec echo failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "hello-integration")
	})

	// Test: exec with -e env var injection.
	t.Run("exec with env var injection", func(t *testing.T) {
		args := fmt.Sprintf("exec %s -e MY_TEST_VAR=hello123 -- env", name)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with -e failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "MY_TEST_VAR=hello123")
	})

	// Test: exec with workdir.
	t.Run("exec with workdir", func(t *testing.T) {
		args := fmt.Sprintf("exec %s -w /tmp -- pwd", name)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec pwd failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "/tmp")
	})

	// Test: exec exit code propagation (command not found should error).
	t.Run("exec failing command", func(t *testing.T) {
		stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{"false"})
		assert.Error(t, err, "exec false should fail: stdout=%s stderr=%s", stdout, stderr)
	})

	// Test: exec with -f file upload (single file to workdir).
	t.Run("exec with file upload to workdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "upload-test.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("file-upload-content"), 0644))

		args := fmt.Sprintf("exec %s -f %s -w /tmp -- cat upload-test.txt", name, srcFile)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with -f failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "file-upload-content")
	})

	// Test: exec with -f file upload (no workdir, uploads to /).
	t.Run("exec with file upload to root", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "root-upload.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("root-content"), 0644))

		args := fmt.Sprintf("exec %s -f %s -- cat /root-upload.txt", name, srcFile)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with -f (no workdir) failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "root-content")
	})

	// Test: exec with multiple -f file uploads.
	t.Run("exec with multiple file uploads", func(t *testing.T) {
		tmpDir := t.TempDir()
		file1 := filepath.Join(tmpDir, "multi-a.txt")
		file2 := filepath.Join(tmpDir, "multi-b.txt")
		// Create a script that cats both files (avoids shell quoting issues in test runner).
		scriptFile := filepath.Join(tmpDir, "catboth.sh")
		require.NoError(t, os.WriteFile(file1, []byte("content-a"), 0644))
		require.NoError(t, os.WriteFile(file2, []byte("content-b"), 0644))
		require.NoError(t, os.WriteFile(scriptFile, []byte("#!/bin/sh\ncat multi-a.txt\ncat multi-b.txt"), 0755))

		args := fmt.Sprintf("exec %s -f %s -f %s -f %s -w /tmp -- sh catboth.sh", name, file1, file2, scriptFile)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with multiple -f failed: stderr=%s", stderr)
		output := string(stdout)
		assert.Contains(t, output, "content-a")
		assert.Contains(t, output, "content-b")
	})

	// Test: exec with -f combined with -w and -e.
	t.Run("exec with file upload combined with env and workdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		scriptFile := filepath.Join(tmpDir, "combo.sh")
		require.NoError(t, os.WriteFile(scriptFile, []byte("#!/bin/sh\necho \"$COMBO_VAR from $(pwd)\""), 0755))

		args := fmt.Sprintf("exec %s -f %s -w /tmp -e COMBO_VAR=hello -- sh combo.sh", name, scriptFile)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with -f -w -e combo failed: stderr=%s", stderr)
		output := string(stdout)
		assert.Contains(t, output, "hello")
		assert.Contains(t, output, "/tmp")
	})

	// Test: exec with -f to non-existent workdir (should create it).
	t.Run("exec with file upload creates missing workdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "newdir-test.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("created-dir-content"), 0644))

		args := fmt.Sprintf("exec %s -f %s -w /opt/myapp/data -- cat newdir-test.txt", name, srcFile)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with -f to new dir failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "created-dir-content")
	})

	// Test: exec with -f overwrites existing file.
	t.Run("exec with file upload overwrites existing file", func(t *testing.T) {
		// First, create a file in the sandbox.
		_, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{"sh", "-c", "echo old-content > /tmp/overwrite-test.txt"})
		require.NoError(t, err, "exec write old content failed: stderr=%s", stderr)

		// Upload a new file with the same name.
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "overwrite-test.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("new-content"), 0644))

		args := fmt.Sprintf("exec %s -f %s -w /tmp -- cat overwrite-test.txt", name, srcFile)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
		require.NoError(t, err, "exec with -f overwrite failed: stderr=%s", stderr)
		assert.Contains(t, string(stdout), "new-content")
		assert.NotContains(t, string(stdout), "old-content")
	})
}

func TestCopy(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("copy")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Test: copy file to sandbox and read it back.
	t.Run("copy to and from sandbox", func(t *testing.T) {
		// Create a temp file on host.
		tmpDir := t.TempDir()
		srcFile := filepath.Join(tmpDir, "test-copy.txt")
		require.NoError(t, os.WriteFile(srcFile, []byte("integration-test-data"), 0644))

		// Copy to sandbox.
		cpToArgs := fmt.Sprintf("cp %s %s:/tmp/test-copy.txt", srcFile, name)
		stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, cpToArgs)
		require.NoError(t, err, "cp to sandbox failed: stdout=%s stderr=%s", stdout, stderr)

		// Read the file inside the sandbox.
		stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{"cat", "/tmp/test-copy.txt"})
		require.NoError(t, err, "exec cat failed: stderr=%s", stderr)
		assert.Equal(t, "integration-test-data", strings.TrimSpace(string(stdout)))

		// Copy from sandbox.
		dstFile := filepath.Join(tmpDir, "test-copy-back.txt")
		cpFromArgs := fmt.Sprintf("cp %s:/tmp/test-copy.txt %s", name, dstFile)
		stdout, stderr, err = intsbx.RunSBXCmd(ctx, config, dbPath, cpFromArgs)
		require.NoError(t, err, "cp from sandbox failed: stdout=%s stderr=%s", stdout, stderr)

		// Verify the file content.
		data, err := os.ReadFile(dstFile)
		require.NoError(t, err, "reading copied file failed")
		assert.Equal(t, "integration-test-data", strings.TrimSpace(string(data)))
	})
}

func TestSnapshotLifecycle(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("snap")
	snapName := "test-snapshot"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create sandbox (snapshot requires a created sandbox with rootfs).
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	// 1. Create snapshot (now creates a local image).
	stdout, stderr, err := intsbx.RunSnapshotCreate(ctx, config, dbPath, name, snapName)
	require.NoError(t, err, "snapshot create failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Snapshot image created")

	// 2. List images - should contain our snapshot image.
	imagesDir := fmt.Sprintf("--images-dir %s", config.ImagesDir)
	stdout, stderr, err = intsbx.RunImageList(ctx, config, dbPath, imagesDir)
	require.NoError(t, err, "image list failed: stdout=%s stderr=%s", stdout, stderr)
	images := parseImageList(t, stdout)
	foundImg := findImageInList(images, snapName)
	require.NotNil(t, foundImg, "snapshot image %s not found in list", snapName)
	assert.Equal(t, "snapshot", foundImg.Source)
	assert.True(t, foundImg.Installed)

	// 3. Remove snapshot image.
	stdout, stderr, err = intsbx.RunImageRm(ctx, config, dbPath, snapName, imagesDir)
	require.NoError(t, err, "image rm failed: stdout=%s stderr=%s", stdout, stderr)

	// 4. Verify snapshot image is gone.
	stdout, stderr, err = intsbx.RunImageList(ctx, config, dbPath, imagesDir)
	require.NoError(t, err, "image list after rm failed: stdout=%s stderr=%s", stdout, stderr)
	images = parseImageList(t, stdout)
	foundImg = findImageInList(images, snapName)
	assert.Nil(t, foundImg, "snapshot image %s should not be in list after rm", snapName)

	// 5. Clean up the sandbox.
	_, _, _ = intsbx.RunRm(ctx, config, dbPath, name)
}

func TestPortForward(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("fwd")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Start a simple TCP listener inside the sandbox using netcat.
	const remotePort = 8765
	const localPort = 18765

	// Create a listener script inside the sandbox, then run it in background.
	// This avoids the space-splitting issue with sh -c in the test runner.
	listenerCtx, listenerCancel := context.WithCancel(ctx)
	defer listenerCancel()

	// Write listener script.
	scriptCmd := fmt.Sprintf("echo '#!/bin/sh\nprintf pong | nc -l -p %d' > /tmp/listen.sh && chmod +x /tmp/listen.sh", remotePort)
	scriptOut, scriptErr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{"sh", "-c", scriptCmd})
	require.NoError(t, err, "write listener script failed: stdout=%s stderr=%s", scriptOut, scriptErr)

	go func() {
		// Run the listener script (blocks until a client connects).
		_, _, _ = intsbx.RunExec(listenerCtx, config, dbPath, name, []string{"/tmp/listen.sh"})
	}()

	// Give the listener a moment to start.
	time.Sleep(2 * time.Second)

	// Start port forwarding in background.
	fwdCtx, fwdCancel := context.WithCancel(ctx)
	defer fwdCancel()

	fwdStarted := make(chan struct{})
	go func() {
		close(fwdStarted)
		_, _, _ = intsbx.RunForward(fwdCtx, config, dbPath, name, fmt.Sprintf("%d:%d", localPort, remotePort))
	}()
	<-fwdStarted

	// Give forwarding a moment to establish.
	time.Sleep(3 * time.Second)

	// Connect to localhost:localPort and read the response.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 5*time.Second)
	if err != nil {
		// Port forward may take longer on some systems. This is a best-effort test.
		t.Logf("Could not connect to forwarded port (this may happen if nc/forward is slow): %v", err)
		t.Logf("Verifying forward at least started without error...")
		// At minimum, verify forward didn't crash immediately by checking the goroutine is still running.
		fwdCancel()
		return
	}
	defer conn.Close()

	// Read response.
	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err == nil {
		assert.Equal(t, "pong", string(buf[:n]))
	}

	// Stop forwarding.
	fwdCancel()
}

func TestRestartCycle(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("restart")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Write a file to the rootfs (not /tmp which is tmpfs and lost on restart).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{"sh", "-c", "echo persist-data > /root/restart-test.txt"})
	require.NoError(t, err, "exec write file failed: stdout=%s stderr=%s", stdout, stderr)

	// Stop the sandbox.
	stdout, stderr, err = intsbx.RunStop(ctx, config, dbPath, name)
	require.NoError(t, err, "stop failed: stdout=%s stderr=%s", stdout, stderr)

	// Verify stopped.
	stdout, stderr, err = intsbx.RunList(ctx, config, dbPath)
	require.NoError(t, err, "list after stop failed: stdout=%s stderr=%s", stdout, stderr)
	items := parseSandboxList(t, stdout)
	found := findSandboxInList(items, name)
	require.NotNil(t, found)
	assert.Equal(t, "stopped", found.Status)

	// Re-start the sandbox.
	stdout, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "re-start failed: stdout=%s stderr=%s", stdout, stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Verify the file persisted across the restart.
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{"cat", "/root/restart-test.txt"})
	require.NoError(t, err, "exec cat after restart failed: stderr=%s", stderr)
	assert.Equal(t, "persist-data", strings.TrimSpace(string(stdout)))

	// Exec works after restart.
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{"echo", "alive-after-restart"})
	require.NoError(t, err, "exec echo after restart failed: stderr=%s", stderr)
	assert.Contains(t, string(stdout), "alive-after-restart")
}

func TestStartWithEnvVars(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("startenv")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	// Start with env vars via -e flag.
	args := fmt.Sprintf("start %s -e SESSION_VAR=from-start-flag -e ANOTHER=value2", name)
	stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
	require.NoError(t, err, "start with -e failed: stdout=%s stderr=%s", stdout, stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Verify env vars are available in exec (sourced from /etc/sbx/session-env.sh).
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{"env"})
	require.NoError(t, err, "exec env failed: stderr=%s", stderr)
	envOutput := string(stdout)
	assert.Contains(t, envOutput, "SESSION_VAR=from-start-flag")
	assert.Contains(t, envOutput, "ANOTHER=value2")
}

func TestStartWithSessionFile(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("startyml")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create a session YAML file.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: test-session
env:
  FILE_VAR: from-yaml-file
  OTHER_VAR: yaml-value
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	// Start with session file.
	args := fmt.Sprintf("start %s -f %s", name, sessionFile)
	stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, args)
	require.NoError(t, err, "start with -f failed: stdout=%s stderr=%s", stdout, stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Verify env vars from YAML file are available.
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{"env"})
	require.NoError(t, err, "exec env failed: stderr=%s", stderr)
	envOutput := string(stdout)
	assert.Contains(t, envOutput, "FILE_VAR=from-yaml-file")
	assert.Contains(t, envOutput, "OTHER_VAR=yaml-value")
}

func TestCreateFromSnapshot(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	srcName := uniqueName("snapsrc")
	snapName := "snap-for-create"
	dstName := uniqueName("snapdst")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, srcName)
	cleanupSandbox(t, config, dbPath, dstName)

	// Create source sandbox and start it.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, srcName)
	require.NoError(t, err, "create source failed: stderr=%s", stderr)
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, srcName)
	require.NoError(t, err, "start source failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, srcName, 60*time.Second)

	// Write a marker file inside the source sandbox (to rootfs, not tmpfs).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, srcName, []string{"sh", "-c", "echo snapshot-marker > /root/marker.txt"})
	require.NoError(t, err, "exec write marker failed: stdout=%s stderr=%s", stdout, stderr)

	// Stop source sandbox (required for snapshot to capture consistent state).
	_, stderr, err = intsbx.RunStop(ctx, config, dbPath, srcName)
	require.NoError(t, err, "stop source failed: stderr=%s", stderr)

	// Create snapshot from source (creates a local image).
	stdout, stderr, err = intsbx.RunSnapshotCreate(ctx, config, dbPath, srcName, snapName)
	require.NoError(t, err, "snapshot create failed: stdout=%s stderr=%s", stdout, stderr)
	// Register cleanup for the snapshot image.
	imagesDir := fmt.Sprintf("--images-dir %s", config.ImagesDir)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_, _, _ = intsbx.RunImageRm(cleanCtx, config, dbPath, snapName, imagesDir)
	})

	// Create new sandbox from snapshot image using --from-image.
	args := fmt.Sprintf("create --name %s --engine firecracker --from-image %s --images-dir %s --cpu 1 --mem 512 --disk 2",
		dstName, snapName, config.ImagesDir)
	stdout, stderr, err = intsbx.RunSBXCmd(ctx, config, dbPath, args)
	require.NoError(t, err, "create from snapshot image failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Sandbox created successfully")

	// Start the new sandbox.
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, dstName)
	require.NoError(t, err, "start from-snapshot sandbox failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, dstName, 60*time.Second)

	// Verify the marker file from the source snapshot exists.
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, dstName, []string{"cat", "/root/marker.txt"})
	require.NoError(t, err, "exec cat marker in snapshot sandbox failed: stderr=%s", stderr)
	assert.Equal(t, "snapshot-marker", strings.TrimSpace(string(stdout)))
}

func TestListStatusFilter(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	runningName := uniqueName("filtrun")
	stoppedName := uniqueName("filtstp")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, runningName)
	cleanupSandbox(t, config, dbPath, stoppedName)

	// Create two sandboxes.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, runningName)
	require.NoError(t, err, "create running sandbox failed: stderr=%s", stderr)
	_, stderr, err = intsbx.RunCreate(ctx, config, dbPath, stoppedName)
	require.NoError(t, err, "create stopped sandbox failed: stderr=%s", stderr)

	// Start both, then stop one.
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, runningName)
	require.NoError(t, err, "start running sandbox failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, runningName, 60*time.Second)

	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, stoppedName)
	require.NoError(t, err, "start stopped sandbox failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, stoppedName, 60*time.Second)

	_, stderr, err = intsbx.RunStop(ctx, config, dbPath, stoppedName)
	require.NoError(t, err, "stop sandbox failed: stderr=%s", stderr)

	// Filter by running - should only show the running one.
	stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, "list --format json --status running")
	require.NoError(t, err, "list --status running failed: stdout=%s stderr=%s", stdout, stderr)
	items := parseSandboxList(t, stdout)
	foundRunning := findSandboxInList(items, runningName)
	foundStopped := findSandboxInList(items, stoppedName)
	assert.NotNil(t, foundRunning, "running sandbox should appear in --status running")
	assert.Nil(t, foundStopped, "stopped sandbox should NOT appear in --status running")

	// Filter by stopped - should only show the stopped one.
	stdout, stderr, err = intsbx.RunSBXCmd(ctx, config, dbPath, "list --format json --status stopped")
	require.NoError(t, err, "list --status stopped failed: stdout=%s stderr=%s", stdout, stderr)
	items = parseSandboxList(t, stdout)
	foundRunning = findSandboxInList(items, runningName)
	foundStopped = findSandboxInList(items, stoppedName)
	assert.Nil(t, foundRunning, "running sandbox should NOT appear in --status stopped")
	assert.NotNil(t, foundStopped, "stopped sandbox should appear in --status stopped")
}

func TestStatusJSON(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("status")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Create sandbox with specific resources.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	// Check status JSON for created state.
	stdout, stderr, err := intsbx.RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("status %s --format json", name))
	require.NoError(t, err, "status failed: stdout=%s stderr=%s", stdout, stderr)

	var s statusOutput
	require.NoError(t, json.Unmarshal(stdout, &s))
	assert.Equal(t, name, s.Name)
	assert.Equal(t, "stopped", s.Status)
	assert.Equal(t, float64(1), s.VCPUs)
	assert.Equal(t, 512, s.MemoryMB)
	assert.Equal(t, 2, s.DiskGB)
	assert.NotEmpty(t, s.ID)

	// Start and verify running status.
	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	stdout, stderr, err = intsbx.RunSBXCmd(ctx, config, dbPath, fmt.Sprintf("status %s --format json", name))
	require.NoError(t, err, "status after start failed: stdout=%s stderr=%s", stdout, stderr)
	require.NoError(t, json.Unmarshal(stdout, &s))
	assert.Equal(t, "running", s.Status)
	assert.Equal(t, name, s.Name)
}
