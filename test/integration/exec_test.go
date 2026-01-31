package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/storage/sqlite"
)

func TestExecCommand(t *testing.T) {
	tests := map[string]struct {
		command      []string
		flags        []string
		expStdout    []string
		expNotStdout []string
		expErr       bool
		expExitCode  int
	}{
		"Simple echo command should succeed": {
			command:     []string{"--", "echo", "hello world"},
			expStdout:   []string{"hello world"},
			expExitCode: 0,
		},

		"Command with exit code 0 should succeed": {
			command:     []string{"--", "sh", "-c", "exit 0"},
			expExitCode: 0,
		},

		"Command with exit code 1 should succeed but return exit 1": {
			command:     []string{"--", "sh", "-c", "exit 1"},
			expExitCode: 1,
		},

		"Working directory flag should set exec directory": {
			command:     []string{"--", "pwd"},
			flags:       []string{"--workdir", "/tmp"},
			expStdout:   []string{"/tmp"},
			expExitCode: 0,
		},

		"Environment variable should be available in command": {
			command:     []string{"--", "sh", "-c", "echo $TEST_VAR"},
			flags:       []string{"--env", "TEST_VAR=test_value"},
			expStdout:   []string{"test_value"},
			expExitCode: 0,
		},

		"Multiple environment variables should work": {
			command:     []string{"--", "sh", "-c", "echo $FOO-$BAR"},
			flags:       []string{"--env", "FOO=hello", "--env", "BAR=world"},
			expStdout:   []string{"hello-world"},
			expExitCode: 0,
		},

		"Command with stdout and stderr should capture both": {
			command:     []string{"--", "sh", "-c", "echo stdout; echo stderr >&2"},
			expStdout:   []string{"stdout", "stderr"},
			expExitCode: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			docker := newDockerHelper(t)

			// Build the binary
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err, "Failed to build sbx binary")
			defer os.Remove("sbx-test")

			// Setup temp DB and create a sandbox
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")
			configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

			// Create sandbox
			createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log")
			err = createCmd.Run()
			require.NoError(t, err, "Failed to create sandbox")

			// Get sandbox ID for cleanup
			repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
				DBPath: dbPath,
			})
			require.NoError(t, err)
			sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
			require.NoError(t, err)
			containerName := getContainerName(sandbox.ID)

			// Register cleanup
			t.Cleanup(func() {
				docker.cleanupContainer(t, containerName)
			})

			// Build exec command
			cmdArgs := []string{"exec", "--db-path", dbPath, "--no-log"}
			cmdArgs = append(cmdArgs, tt.flags...)
			cmdArgs = append(cmdArgs, "example-sandbox")
			cmdArgs = append(cmdArgs, tt.command...)

			// Execute command
			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err = cmd.Run()

			// Check exit code
			if tt.expExitCode != 0 {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr)
				assert.Equal(t, tt.expExitCode, exitErr.ExitCode())
			} else {
				if tt.expErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}

			// Check stdout
			stdoutStr := stdout.String() + stderr.String()
			for _, exp := range tt.expStdout {
				assert.Contains(t, stdoutStr, exp)
			}
			for _, notExp := range tt.expNotStdout {
				assert.NotContains(t, stdoutStr, notExp)
			}
		})
	}
}

func TestExecCommandErrors(t *testing.T) {
	tests := map[string]struct {
		sandboxName string
		command     []string
		expErr      bool
	}{
		"Exec on nonexistent sandbox should fail": {
			sandboxName: "nonexistent",
			command:     []string{"echo", "hello"},
			expErr:      true,
		},

		"Empty command should fail": {
			sandboxName: "example-sandbox",
			command:     []string{},
			expErr:      true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			docker := newDockerHelper(t)

			// Build the binary
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err, "Failed to build sbx binary")
			defer os.Remove("sbx-test")

			// Setup temp DB
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			var containerName string

			// Create sandbox if testing with existing sandbox
			if !strings.Contains(name, "nonexistent") {
				configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")
				createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log")
				err = createCmd.Run()
				require.NoError(t, err, "Failed to create sandbox")

				// Get sandbox ID for cleanup
				repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
					DBPath: dbPath,
				})
				require.NoError(t, err)
				sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
				require.NoError(t, err)
				containerName = getContainerName(sandbox.ID)
			}

			// Register cleanup if sandbox was created
			if containerName != "" {
				t.Cleanup(func() {
					docker.cleanupContainer(t, containerName)
				})
			}

			// Build exec command
			cmdArgs := []string{"exec", "--db-path", dbPath, "--no-log", tt.sandboxName}
			cmdArgs = append(cmdArgs, tt.command...)

			// Execute command
			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err = cmd.Run()

			if tt.expErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExecOnStoppedSandbox(t *testing.T) {
	docker := newDockerHelper(t)

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build sbx binary")
	defer os.Remove("sbx-test")

	// Setup temp DB and create sandbox
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

	// Create sandbox
	createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log")
	err = createCmd.Run()
	require.NoError(t, err, "Failed to create sandbox")

	// Get sandbox ID for cleanup
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
	})
	require.NoError(t, err)
	sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
	require.NoError(t, err)
	containerName := getContainerName(sandbox.ID)

	// Register cleanup
	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	// Stop sandbox
	stopCmd := exec.Command("./sbx-test", "stop", "--db-path", dbPath, "--no-log", "example-sandbox")
	err = stopCmd.Run()
	require.NoError(t, err, "Failed to stop sandbox")

	// Try to exec on stopped sandbox (should fail)
	execCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log", "example-sandbox", "echo", "hello")
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err = execCmd.Run()
	assert.Error(t, err, "Exec on stopped sandbox should fail")
	assert.Contains(t, stderr.String(), "not running")
}

func TestShellCommand(t *testing.T) {
	// Note: This test only validates that the shell command runs without error
	// We can't test interactive behavior in automated tests
	docker := newDockerHelper(t)

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build sbx binary")
	defer os.Remove("sbx-test")

	// Setup temp DB and create sandbox
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

	// Create sandbox
	createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log")
	err = createCmd.Run()
	require.NoError(t, err, "Failed to create sandbox")

	// Get sandbox ID for cleanup
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
	})
	require.NoError(t, err)
	sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
	require.NoError(t, err)
	containerName := getContainerName(sandbox.ID)

	// Register cleanup
	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	// Test shell command by piping a command to exit immediately
	shellCmd := exec.Command("./sbx-test", "shell", "--db-path", dbPath, "--no-log", "example-sandbox")
	shellCmd.Stdin = strings.NewReader("exit\n")
	var stdout, stderr bytes.Buffer
	shellCmd.Stdout = &stdout
	shellCmd.Stderr = &stderr

	err = shellCmd.Run()
	// Shell will exit with code 0 since we exit cleanly
	assert.NoError(t, err, "Shell command should execute without error")
}

func TestExecRunsInContainer(t *testing.T) {
	// This test verifies that exec commands actually run inside the Docker container
	// by creating a file and then verifying it exists within the container
	docker := newDockerHelper(t)

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build sbx binary")
	defer os.Remove("sbx-test")

	// Setup temp DB and create sandbox
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

	// Create sandbox
	createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log")
	err = createCmd.Run()
	require.NoError(t, err, "Failed to create sandbox")

	// Get sandbox ID for cleanup
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
	})
	require.NoError(t, err)
	sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
	require.NoError(t, err)
	containerName := getContainerName(sandbox.ID)

	// Register cleanup
	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	// Test 1: Create a unique file with a timestamp
	uniqueContent := "sbx-test-content-" + sandbox.ID
	createFileCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log", "example-sandbox",
		"--", "sh", "-c", "echo '"+uniqueContent+"' > /tmp/sbx-test-file.txt")
	var createStderr bytes.Buffer
	createFileCmd.Stderr = &createStderr
	err = createFileCmd.Run()
	if err != nil {
		t.Logf("Create file stderr: %s", createStderr.String())
	}
	require.NoError(t, err, "Failed to create file in container")

	// Test 2: Read the file back to verify it was created inside the container
	readFileCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log", "example-sandbox",
		"--", "cat", "/tmp/sbx-test-file.txt")
	var stdout bytes.Buffer
	readFileCmd.Stdout = &stdout
	err = readFileCmd.Run()
	require.NoError(t, err, "Failed to read file from container")

	// Verify the content matches what we wrote
	assert.Contains(t, stdout.String(), uniqueContent, "File content should match what we wrote")

	// Test 3: Verify we can use the file in subsequent commands (proves persistence within container)
	wcCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log", "example-sandbox",
		"--", "wc", "-l", "/tmp/sbx-test-file.txt")
	var wcOutput bytes.Buffer
	wcCmd.Stdout = &wcOutput
	err = wcCmd.Run()
	require.NoError(t, err, "Failed to run wc command")
	assert.Contains(t, wcOutput.String(), "1", "File should have 1 line")

	// Test 4: Verify working directory works correctly by creating a file in /tmp and checking pwd
	pwdCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log",
		"--workdir", "/tmp", "example-sandbox", "--", "pwd")
	var pwdOutput bytes.Buffer
	pwdCmd.Stdout = &pwdOutput
	err = pwdCmd.Run()
	require.NoError(t, err, "Failed to check working directory")
	assert.Contains(t, pwdOutput.String(), "/tmp", "Working directory should be /tmp")

	// Test 5: Verify we can list the file we created earlier (proves exec persistence)
	lsCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log",
		"--workdir", "/tmp", "example-sandbox", "--", "ls", "-la", "sbx-test-file.txt")
	var lsOutput bytes.Buffer
	lsCmd.Stdout = &lsOutput
	err = lsCmd.Run()
	require.NoError(t, err, "Failed to list file")
	assert.Contains(t, lsOutput.String(), "sbx-test-file.txt", "Should be able to see the file we created")

	// Test 6: Verify environment variable is actually set in the container
	envCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log",
		"--env", "SBX_TEST=container-value", "example-sandbox",
		"--", "sh", "-c", "echo \"$SBX_TEST\"")
	var envOutput bytes.Buffer
	envCmd.Stdout = &envOutput
	envCmd.Stderr = &envOutput
	err = envCmd.Run()
	if err != nil {
		t.Logf("Env command output: %s", envOutput.String())
	}
	require.NoError(t, err, "Failed to check environment variable")
	assert.Contains(t, envOutput.String(), "container-value", "Environment variable should be set in container")
}

func TestExecCommandActuallyRunsInDocker(t *testing.T) {
	// This test uses docker inspect to verify commands run in the actual container
	docker := newDockerHelper(t)

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
	err := buildCmd.Run()
	require.NoError(t, err, "Failed to build sbx binary")
	defer os.Remove("sbx-test")

	// Setup temp DB and create sandbox
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

	// Create sandbox
	createCmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath, "--no-log")
	err = createCmd.Run()
	require.NoError(t, err, "Failed to create sandbox")

	// Get sandbox ID for cleanup
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
	})
	require.NoError(t, err)
	sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
	require.NoError(t, err)
	containerName := getContainerName(sandbox.ID)

	// Register cleanup
	t.Cleanup(func() {
		docker.cleanupContainer(t, containerName)
	})

	// Execute a command that creates a file via sbx exec
	createCmd2 := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log", "example-sandbox",
		"--", "touch", "/tmp/docker-proof-file")
	err = createCmd2.Run()
	require.NoError(t, err, "Failed to create proof file")

	// Now verify the file exists by using docker exec directly (not sbx)
	dockerExecCmd := exec.Command("docker", "exec", containerName, "test", "-f", "/tmp/docker-proof-file")
	err = dockerExecCmd.Run()
	assert.NoError(t, err, "File created via sbx exec should be visible via direct docker exec")

	// Create another file via direct docker exec
	dockerCreateCmd := exec.Command("docker", "exec", containerName, "touch", "/tmp/docker-direct-file")
	err = dockerCreateCmd.Run()
	require.NoError(t, err, "Failed to create file via docker")

	// Verify we can see it via sbx exec
	sbxTestCmd := exec.Command("./sbx-test", "exec", "--db-path", dbPath, "--no-log", "example-sandbox",
		"--", "test", "-f", "/tmp/docker-direct-file")
	err = sbxTestCmd.Run()
	assert.NoError(t, err, "File created via docker exec should be visible via sbx exec")
}
