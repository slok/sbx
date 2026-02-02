package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/storage/sqlite"
)

func TestCopyCommand(t *testing.T) {
	docker := newDockerHelper(t)

	// Cleanup any leftover containers before starting.
	docker.cleanupAllSbxContainers(t)

	buildTestBinary(t)

	tests := map[string]struct {
		setupFiles     func(t *testing.T, tmpDir string) (srcPath, dstArg string)
		copyDirection  string // "to" or "from"
		expErr         bool
		expStdout      []string
		validateResult func(t *testing.T, tmpDir string)
	}{
		"CopyTo file succeeds": {
			copyDirection: "to",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				// Create a local file to copy.
				srcPath := filepath.Join(tmpDir, "test-file.txt")
				err := os.WriteFile(srcPath, []byte("hello from host"), 0644)
				require.NoError(t, err)
				return srcPath, "copy-test-sandbox:/tmp/test-file.txt"
			},
			expStdout: []string{
				"Copied",
				"copy-test-sandbox:/tmp/test-file.txt",
			},
			validateResult: func(t *testing.T, tmpDir string) {
				// Verify file exists in container by running exec.
				dbPath := filepath.Join(tmpDir, "test.db")
				cmd := exec.Command("./sbx-test", "exec", "copy-test-sandbox", "--db-path", dbPath, "--no-log", "--", "cat", "/tmp/test-file.txt")
				var stdout bytes.Buffer
				cmd.Stdout = &stdout
				err := cmd.Run()
				require.NoError(t, err)
				assert.Equal(t, "hello from host", stdout.String())
			},
		},

		"CopyTo directory succeeds": {
			copyDirection: "to",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				// Create a local directory with files to copy.
				srcDir := filepath.Join(tmpDir, "test-dir")
				err := os.MkdirAll(srcDir, 0755)
				require.NoError(t, err)

				err = os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("file1 content"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(srcDir, "file2.txt"), []byte("file2 content"), 0644)
				require.NoError(t, err)

				return srcDir, "copy-test-sandbox:/tmp/test-dir"
			},
			expStdout: []string{
				"Copied",
				"copy-test-sandbox:/tmp/test-dir",
			},
			validateResult: func(t *testing.T, tmpDir string) {
				// Verify files exist in container.
				dbPath := filepath.Join(tmpDir, "test.db")
				cmd := exec.Command("./sbx-test", "exec", "copy-test-sandbox", "--db-path", dbPath, "--no-log", "--", "cat", "/tmp/test-dir/file1.txt")
				var stdout bytes.Buffer
				cmd.Stdout = &stdout
				err := cmd.Run()
				require.NoError(t, err)
				assert.Equal(t, "file1 content", stdout.String())
			},
		},

		"CopyFrom file succeeds": {
			copyDirection: "from",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				// Create a file in the container first.
				dbPath := filepath.Join(tmpDir, "test.db")
				cmd := exec.Command("./sbx-test", "exec", "copy-test-sandbox", "--db-path", dbPath, "--no-log", "--", "sh", "-c", "echo -n 'hello from container' > /tmp/remote-file.txt")
				err := cmd.Run()
				require.NoError(t, err)

				dstPath := filepath.Join(tmpDir, "local-file.txt")
				return "copy-test-sandbox:/tmp/remote-file.txt", dstPath
			},
			expStdout: []string{
				"Copied",
				"copy-test-sandbox:/tmp/remote-file.txt",
			},
			validateResult: func(t *testing.T, tmpDir string) {
				// Verify file was copied to host.
				content, err := os.ReadFile(filepath.Join(tmpDir, "local-file.txt"))
				require.NoError(t, err)
				assert.Equal(t, "hello from container", string(content))
			},
		},

		"CopyTo with nonexistent source fails": {
			copyDirection: "to",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				return "/nonexistent/path/file.txt", "copy-test-sandbox:/tmp/"
			},
			expErr: true,
		},

		"Copy with invalid syntax (no colon) fails": {
			copyDirection: "invalid",
			setupFiles: func(t *testing.T, tmpDir string) (string, string) {
				srcPath := filepath.Join(tmpDir, "test-file.txt")
				err := os.WriteFile(srcPath, []byte("test"), 0644)
				require.NoError(t, err)
				return srcPath, filepath.Join(tmpDir, "other.txt")
			},
			expErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")
			configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

			// Create a sandbox first.
			createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log", "--name", "copy-test-sandbox")
			err := createCmd.Run()
			require.NoError(t, err)

			// Get container name for cleanup.
			repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
				DBPath: dbPath,
			})
			require.NoError(t, err)
			sandbox, err := repo.GetSandboxByName(context.Background(), "copy-test-sandbox")
			require.NoError(t, err)
			containerName := getContainerName(sandbox.ID)

			t.Cleanup(func() {
				docker.cleanupContainer(t, containerName)
			})

			// Setup test files.
			src, dst := tt.setupFiles(t, tmpDir)

			// Build copy command args.
			cmdArgs := []string{"cp", src, dst, "--db-path", dbPath, "--no-log"}

			// Execute copy.
			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()

			if tt.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stdout: %s, stderr: %s", stdout.String(), stderr.String())

				// Check stdout.
				stdoutStr := stdout.String()
				for _, exp := range tt.expStdout {
					assert.Contains(t, stdoutStr, exp)
				}

				// Validate result.
				if tt.validateResult != nil {
					tt.validateResult(t, tmpDir)
				}
			}
		})
	}
}

func TestCopyOnStoppedSandbox(t *testing.T) {
	docker := newDockerHelper(t)
	docker.cleanupAllSbxContainers(t)

	buildTestBinary(t)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

	// Create and stop a sandbox.
	createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log", "--name", "stopped-sandbox")
	err := createCmd.Run()
	require.NoError(t, err)

	// Get container name for cleanup.
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
	})
	require.NoError(t, err)
	sandbox, err := repo.GetSandboxByName(context.Background(), "stopped-sandbox")
	require.NoError(t, err)
	containerName := getContainerName(sandbox.ID)

	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	// Stop the sandbox.
	stopCmd := exec.Command("./sbx-test", "stop", "stopped-sandbox", "--db-path", dbPath, "--no-log")
	err = stopCmd.Run()
	require.NoError(t, err)
	docker.waitForContainerStopped(t, containerName)

	// Create a local file to copy.
	srcPath := filepath.Join(tmpDir, "test-file.txt")
	err = os.WriteFile(srcPath, []byte("test"), 0644)
	require.NoError(t, err)

	// Try to copy to stopped sandbox - should fail.
	cmd := exec.Command("./sbx-test", "cp", srcPath, "stopped-sandbox:/tmp/", "--db-path", dbPath, "--no-log")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()

	require.Error(t, err)
	assert.Contains(t, stderr.String(), "not running")
}

func TestCopyToNonexistentSandbox(t *testing.T) {
	buildTestBinary(t)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a local file to copy.
	srcPath := filepath.Join(tmpDir, "test-file.txt")
	err := os.WriteFile(srcPath, []byte("test"), 0644)
	require.NoError(t, err)

	// Try to copy to nonexistent sandbox.
	cmd := exec.Command("./sbx-test", "cp", srcPath, "nonexistent:/tmp/", "--db-path", dbPath, "--no-log")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()

	require.Error(t, err)
	assert.Contains(t, stderr.String(), "could not find sandbox")
}
