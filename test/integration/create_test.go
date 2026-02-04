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

func TestCreateCommand(t *testing.T) {
	tests := map[string]struct {
		args         []string
		expErr       bool
		expStdout    []string
		expNotStdout []string
		validateDB   func(t *testing.T, dbPath string)
	}{
		"Successful creation with docker engine": {
			args: []string{"--name", "example-sandbox", "--engine", "docker", "--docker-image", "ubuntu:22.04"},
			expStdout: []string{
				"Sandbox created successfully!",
				"Name:   example-sandbox",
				"Status: created",
				"Engine: docker",
				"Image:  ubuntu:22.04",
			},
			validateDB: func(t *testing.T, dbPath string) {
				repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
					DBPath: dbPath,
				})
				require.NoError(t, err)

				sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
				require.NoError(t, err)
				assert.Equal(t, "example-sandbox", sandbox.Name)
				assert.NotNil(t, sandbox.Config.DockerEngine)
				assert.Equal(t, "ubuntu:22.04", sandbox.Config.DockerEngine.Image)
			},
		},
		"Custom resource flags work": {
			args: []string{"--name", "custom-name", "--engine", "docker", "--docker-image", "ubuntu:22.04", "--cpu", "4", "--mem", "4096", "--disk", "20"},
			expStdout: []string{
				"Sandbox created successfully!",
				"Name:   custom-name",
			},
			expNotStdout: []string{
				"example-sandbox",
			},
			validateDB: func(t *testing.T, dbPath string) {
				repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
					DBPath: dbPath,
				})
				require.NoError(t, err)

				sandbox, err := repo.GetSandboxByName(context.Background(), "custom-name")
				require.NoError(t, err)
				assert.Equal(t, "custom-name", sandbox.Name)
			},
		},
		"Duplicate name fails": {
			args:   []string{"--name", "example-sandbox", "--engine", "docker", "--docker-image", "ubuntu:22.04"},
			expErr: true,
		},
		"Missing required flags fails": {
			args:   []string{"--name", "test"},
			expErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			docker := newDockerHelper(t)

			// Build the binary first
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err, "Failed to build sbx binary")
			defer os.Remove("sbx-test")

			// Setup temp DB
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			// If test name is "Duplicate name fails", create the first sandbox
			var firstContainerName string
			if strings.Contains(name, "Duplicate") {
				cmd := exec.Command("./sbx-test", "create", "--name", "example-sandbox", "--engine", "docker", "--docker-image", "ubuntu:22.04", "--db-path", dbPath, "--no-log")
				err := cmd.Run()
				require.NoError(t, err)

				// Get the first sandbox ID for cleanup
				repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
					DBPath: dbPath,
				})
				require.NoError(t, err)
				sandbox, err := repo.GetSandboxByName(context.Background(), "example-sandbox")
				require.NoError(t, err)
				firstContainerName = getContainerName(sandbox.ID)

				// Register cleanup for first container
				t.Cleanup(func() {
					docker.cleanupContainer(t, firstContainerName)
				})
			}

			// Build command args
			cmdArgs := []string{"create", "--db-path", dbPath, "--no-log"}
			cmdArgs = append(cmdArgs, tt.args...)

			// Execute
			cmd := exec.Command("./sbx-test", cmdArgs...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()

			// Verify
			if tt.expErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "stderr: %s", stderr.String())

				// Check stdout
				stdoutStr := stdout.String()
				for _, exp := range tt.expStdout {
					assert.Contains(t, stdoutStr, exp)
				}
				for _, notExp := range tt.expNotStdout {
					assert.NotContains(t, stdoutStr, notExp)
				}

				// Validate DB if needed
				if tt.validateDB != nil {
					tt.validateDB(t, dbPath)
				}

				// Validate Docker container was created (but NOT started - create only provisions).
				repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
					DBPath: dbPath,
				})
				require.NoError(t, err)

				// Determine sandbox name from test
				sandboxName := "example-sandbox"
				for i, arg := range tt.args {
					if arg == "--name" && i+1 < len(tt.args) {
						sandboxName = tt.args[i+1]
						break
					}
				}

				sandbox, err := repo.GetSandboxByName(context.Background(), sandboxName)
				require.NoError(t, err)
				containerName := getContainerName(sandbox.ID)

				// Verify Docker container exists but is NOT running (create only).
				docker.requireContainerExists(t, containerName)
				docker.requireContainerStopped(t, containerName)

				// Register cleanup
				t.Cleanup(func() {
					docker.cleanupContainer(t, containerName)
				})
			}
		})
	}
}
