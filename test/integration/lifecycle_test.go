package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/sqlite"
)

// TestMain runs before all tests and after all tests for cleanup
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()

	// Cleanup any leftover containers after all tests
	// Note: Individual tests also cleanup their own containers
	os.Exit(code)
}

func buildTestBinary(t *testing.T) {
	buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
	err := buildCmd.Run()
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Remove("sbx-test")
	})
}

func TestListCommand(t *testing.T) {
	tests := map[string]struct {
		setupSandboxes func(t *testing.T, dbPath string)
		args           []string
		expErr         bool
		expStdout      []string
		expNotStdout   []string
		validateJSON   func(t *testing.T, output string)
	}{
		"Empty list shows nothing": {
			setupSandboxes: func(t *testing.T, dbPath string) {
				// No sandboxes
			},
			args: []string{},
			// Empty list produces no output
		},
		"List shows multiple sandboxes": {
			setupSandboxes: func(t *testing.T, dbPath string) {
				createSandbox(t, dbPath, "sandbox-1")
				createSandbox(t, dbPath, "sandbox-2")
			},
			args: []string{},
			expStdout: []string{
				"sandbox-1",
				"sandbox-2",
				"running",
			},
		},
		"Filter by running status": {
			setupSandboxes: func(t *testing.T, dbPath string) {
				createSandbox(t, dbPath, "running-box")
				createAndStopSandbox(t, dbPath, "stopped-box")
			},
			args: []string{"--status", "running"},
			expStdout: []string{
				"running-box",
			},
			expNotStdout: []string{
				"stopped-box",
			},
		},
		"Filter by stopped status": {
			setupSandboxes: func(t *testing.T, dbPath string) {
				createSandbox(t, dbPath, "running-box")
				createAndStopSandbox(t, dbPath, "stopped-box")
			},
			args: []string{"--status", "stopped"},
			expStdout: []string{
				"stopped-box",
			},
			expNotStdout: []string{
				"running-box",
			},
		},
		"JSON format output": {
			setupSandboxes: func(t *testing.T, dbPath string) {
				createSandbox(t, dbPath, "json-test")
			},
			args: []string{"--format", "json"},
			validateJSON: func(t *testing.T, output string) {
				var sandboxes []map[string]interface{}
				err := json.Unmarshal([]byte(output), &sandboxes)
				require.NoError(t, err)
				require.Len(t, sandboxes, 1)
				assert.Equal(t, "json-test", sandboxes[0]["name"])
				assert.Equal(t, "running", sandboxes[0]["status"])
				assert.NotEmpty(t, sandboxes[0]["id"])
				assert.NotEmpty(t, sandboxes[0]["created_at"])
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			buildTestBinary(t)

			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			tt.setupSandboxes(t, dbPath)

			cmdArgs := []string{"list", "--db-path", dbPath, "--no-log"}
			cmdArgs = append(cmdArgs, tt.args...)

			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()

			if tt.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stderr.String())

				stdoutStr := stdout.String()
				for _, exp := range tt.expStdout {
					assert.Contains(t, stdoutStr, exp)
				}
				for _, notExp := range tt.expNotStdout {
					assert.NotContains(t, stdoutStr, notExp)
				}

				if tt.validateJSON != nil {
					tt.validateJSON(t, stdoutStr)
				}
			}
		})
	}
}

func TestStatusCommand(t *testing.T) {
	tests := map[string]struct {
		setupSandboxes func(t *testing.T, dbPath string) string // Returns sandbox name/ID
		args           []string
		expErr         bool
		expStdout      []string
		expNotStdout   []string
		validateJSON   func(t *testing.T, output string)
	}{
		"Status by name": {
			setupSandboxes: func(t *testing.T, dbPath string) string {
				createSandbox(t, dbPath, "test-sandbox")
				return "test-sandbox"
			},
			args: []string{},
			expStdout: []string{
				"Name:       test-sandbox",
				"ID:",
				"Status:     running",
				"Engine:     docker",
				"Image:      ubuntu:22.04",
				"VCPUs:      2",
				"Memory:     2048 MB",
				"Disk:       10 GB",
				"Created:",
				"Started:",
			},
		},
		"Status by ID": {
			setupSandboxes: func(t *testing.T, dbPath string) string {
				id := createSandbox(t, dbPath, "id-test")
				return id
			},
			args: []string{},
			expStdout: []string{
				"Name:       id-test",
				"Status:     running",
			},
		},
		"Status with JSON format": {
			setupSandboxes: func(t *testing.T, dbPath string) string {
				createSandbox(t, dbPath, "json-sandbox")
				return "json-sandbox"
			},
			args: []string{"--format", "json"},
			validateJSON: func(t *testing.T, output string) {
				var sandbox map[string]interface{}
				err := json.Unmarshal([]byte(output), &sandbox)
				require.NoError(t, err)
				assert.Equal(t, "json-sandbox", sandbox["name"])
				assert.Equal(t, "running", sandbox["status"])
				// Check engine info
				engine, ok := sandbox["engine"].(map[string]interface{})
				require.True(t, ok, "engine should be a map")
				assert.Equal(t, "docker", engine["type"])
				assert.Equal(t, "ubuntu:22.04", engine["image"])
			},
		},
		"Nonexistent sandbox fails": {
			setupSandboxes: func(t *testing.T, dbPath string) string {
				return "nonexistent"
			},
			args:   []string{},
			expErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err)
			defer os.Remove("sbx-test")

			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			identifier := tt.setupSandboxes(t, dbPath)

			cmdArgs := []string{"status", identifier, "--db-path", dbPath, "--no-log"}
			cmdArgs = append(cmdArgs, tt.args...)

			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()

			if tt.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stderr.String())

				stdoutStr := stdout.String()
				for _, exp := range tt.expStdout {
					assert.Contains(t, stdoutStr, exp)
				}
				for _, notExp := range tt.expNotStdout {
					assert.NotContains(t, stdoutStr, notExp)
				}

				if tt.validateJSON != nil {
					tt.validateJSON(t, stdoutStr)
				}
			}
		})
	}
}

func TestStartStopCommands(t *testing.T) {
	tests := map[string]struct {
		setupSandbox func(t *testing.T, dbPath string) string
		command      string
		expErr       bool
		expStdout    []string
		validateDB   func(t *testing.T, dbPath, sandboxName string)
	}{
		"Stop running sandbox succeeds": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createSandbox(t, dbPath, "stop-test")
				return "stop-test"
			},
			command: "stop",
			expStdout: []string{
				"Stopped sandbox: stop-test",
			},
			validateDB: func(t *testing.T, dbPath, sandboxName string) {
				sandbox := getSandboxByName(t, dbPath, sandboxName)
				assert.Equal(t, model.SandboxStatusStopped, sandbox.Status)
			},
		},
		"Start stopped sandbox succeeds": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createAndStopSandbox(t, dbPath, "start-test")
				return "start-test"
			},
			command: "start",
			expStdout: []string{
				"Started sandbox: start-test",
			},
			validateDB: func(t *testing.T, dbPath, sandboxName string) {
				sandbox := getSandboxByName(t, dbPath, sandboxName)
				assert.Equal(t, model.SandboxStatusRunning, sandbox.Status)
			},
		},
		"Stop already stopped sandbox fails": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createAndStopSandbox(t, dbPath, "already-stopped")
				return "already-stopped"
			},
			command: "stop",
			expErr:  true,
		},
		"Start already running sandbox fails": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createSandbox(t, dbPath, "already-running")
				return "already-running"
			},
			command: "start",
			expErr:  true,
		},
		"Stop nonexistent sandbox fails": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				return "nonexistent"
			},
			command: "stop",
			expErr:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err)
			defer os.Remove("sbx-test")

			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			sandboxName := tt.setupSandbox(t, dbPath)

			cmdArgs := []string{tt.command, sandboxName, "--db-path", dbPath, "--no-log"}

			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()

			if tt.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stderr.String())

				stdoutStr := stdout.String()
				for _, exp := range tt.expStdout {
					assert.Contains(t, stdoutStr, exp)
				}

				if tt.validateDB != nil {
					tt.validateDB(t, dbPath, sandboxName)
				}
			}
		})
	}
}

func TestRemoveCommand(t *testing.T) {
	tests := map[string]struct {
		setupSandbox func(t *testing.T, dbPath string) string
		args         []string
		expErr       bool
		expStdout    []string
		validateDB   func(t *testing.T, dbPath, sandboxName string)
	}{
		"Remove stopped sandbox succeeds": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createAndStopSandbox(t, dbPath, "remove-test")
				return "remove-test"
			},
			args: []string{},
			expStdout: []string{
				"Removed sandbox: remove-test",
			},
			validateDB: func(t *testing.T, dbPath, sandboxName string) {
				repo := getRepository(t, dbPath)
				_, err := repo.GetSandboxByName(context.Background(), sandboxName)
				assert.Error(t, err) // Should not exist
			},
		},
		"Remove running sandbox without force fails": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createSandbox(t, dbPath, "running-remove")
				return "running-remove"
			},
			args:   []string{},
			expErr: true,
		},
		"Remove running sandbox with force succeeds": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				createSandbox(t, dbPath, "force-remove")
				return "force-remove"
			},
			args: []string{"--force"},
			expStdout: []string{
				"Stopped and removed sandbox: force-remove",
			},
			validateDB: func(t *testing.T, dbPath, sandboxName string) {
				repo := getRepository(t, dbPath)
				_, err := repo.GetSandboxByName(context.Background(), sandboxName)
				assert.Error(t, err)
			},
		},
		"Remove nonexistent sandbox fails": {
			setupSandbox: func(t *testing.T, dbPath string) string {
				return "nonexistent"
			},
			args:   []string{},
			expErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err)
			defer os.Remove("sbx-test")

			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			sandboxName := tt.setupSandbox(t, dbPath)

			cmdArgs := []string{"rm", sandboxName, "--db-path", dbPath, "--no-log"}
			cmdArgs = append(cmdArgs, tt.args...)

			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()

			if tt.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stderr.String())

				stdoutStr := stdout.String()
				for _, exp := range tt.expStdout {
					assert.Contains(t, stdoutStr, exp)
				}

				if tt.validateDB != nil {
					tt.validateDB(t, dbPath, sandboxName)
				}
			}
		})
	}
}

func TestCompleteLifecycle(t *testing.T) {
	docker := newDockerHelper(t)

	// Cleanup any leftover containers before starting
	docker.cleanupAllSbxContainers(t)

	buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
	err := buildCmd.Run()
	require.NoError(t, err)
	defer os.Remove("sbx-test")

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

	// Create
	cmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log", "--name", "lifecycle-test")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "lifecycle-test")

	// Get sandbox ID for Docker verification
	repo := getRepository(t, dbPath)
	sandbox, err := repo.GetSandboxByName(context.Background(), "lifecycle-test")
	require.NoError(t, err)
	containerName := getContainerName(sandbox.ID)

	// Verify Docker container exists and is running
	docker.requireContainerExists(t, containerName)
	docker.requireContainerRunning(t, containerName)

	// Register cleanup
	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	// List - should show as running
	cmd = exec.Command("./sbx-test", "list", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "lifecycle-test")
	assert.Contains(t, stdout.String(), "running")

	// Status
	cmd = exec.Command("./sbx-test", "status", "lifecycle-test", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "lifecycle-test")
	assert.Contains(t, stdout.String(), "Status:     running")

	// Stop
	cmd = exec.Command("./sbx-test", "stop", "lifecycle-test", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Stopped sandbox: lifecycle-test")

	// Wait for Docker container to stop (docker stop can take up to 10 seconds)
	docker.waitForContainerStopped(t, containerName)

	// Verify Docker container is stopped
	docker.requireContainerExists(t, containerName)
	docker.requireContainerStopped(t, containerName)

	// List - should show as stopped
	cmd = exec.Command("./sbx-test", "list", "--status", "stopped", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "lifecycle-test")

	// Start
	cmd = exec.Command("./sbx-test", "start", "lifecycle-test", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Started sandbox: lifecycle-test")

	// Verify Docker container is running again
	docker.requireContainerRunning(t, containerName)

	// Remove with force (since it's running)
	cmd = exec.Command("./sbx-test", "rm", "lifecycle-test", "--force", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Stopped and removed sandbox: lifecycle-test")

	// Verify Docker container is removed
	docker.requireContainerNotExists(t, containerName)

	// List - should be empty (no header when empty)
	cmd = exec.Command("./sbx-test", "list", "--db-path", dbPath, "--no-log")
	stdout.Reset()
	cmd.Stdout = &stdout
	err = cmd.Run()
	require.NoError(t, err)
	output := stdout.String()
	assert.NotContains(t, output, "lifecycle-test")
	// Empty list produces no output
	assert.Empty(t, output)
}

// Helper functions

func createSandbox(t *testing.T, dbPath, name string) string {
	docker := newDockerHelper(t)

	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")
	cmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log", "--name", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "Failed to create sandbox: %s", stderr.String())

	// Get the sandbox to return its ID
	repo := getRepository(t, dbPath)
	sandbox, err := repo.GetSandboxByName(context.Background(), name)
	require.NoError(t, err)

	// Verify Docker container was created and is running
	containerName := getContainerName(sandbox.ID)
	docker.requireContainerExists(t, containerName)
	docker.requireContainerRunning(t, containerName)

	// Register cleanup to remove container when test finishes
	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	return sandbox.ID
}

func createAndStopSandbox(t *testing.T, dbPath, name string) string {
	docker := newDockerHelper(t)

	id := createSandbox(t, dbPath, name)
	cmd := exec.Command("./sbx-test", "stop", name, "--db-path", dbPath, "--no-log")
	err := cmd.Run()
	require.NoError(t, err)

	// Wait for Docker container to stop
	containerName := getContainerName(id)
	docker.waitForContainerStopped(t, containerName)

	// Verify Docker container is stopped (not running)
	docker.requireContainerExists(t, containerName)
	docker.requireContainerStopped(t, containerName)

	return id
}

func getSandboxByName(t *testing.T, dbPath, name string) *model.Sandbox {
	repo := getRepository(t, dbPath)
	sandbox, err := repo.GetSandboxByName(context.Background(), name)
	require.NoError(t, err)
	return sandbox
}

func getRepository(t *testing.T, dbPath string) *sqlite.Repository {
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
	})
	require.NoError(t, err)
	return repo
}
