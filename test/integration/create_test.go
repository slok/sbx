package integration_test

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
		setupConfig  func(t *testing.T) string
		args         []string
		expErr       bool
		expStdout    []string
		expNotStdout []string
		validateDB   func(t *testing.T, dbPath string)
	}{
		"Successful creation with example config": {
			setupConfig: func(t *testing.T) string {
				return filepath.Join("..", "..", "testdata", "sandbox.yaml")
			},
			args: []string{},
			expStdout: []string{
				"Sandbox created successfully!",
				"Name:   example-sandbox",
				"Status: running",
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
		"Name override works": {
			setupConfig: func(t *testing.T) string {
				return filepath.Join("..", "..", "testdata", "sandbox.yaml")
			},
			args: []string{"--name", "custom-name"},
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
			setupConfig: func(t *testing.T) string {
				// Create first sandbox
				configPath := filepath.Join("..", "..", "testdata", "sandbox.yaml")

				return configPath
			},
			args:   []string{},
			expErr: true,
		},
		"Missing config file fails": {
			setupConfig: func(t *testing.T) string {
				return "/nonexistent/path/config.yaml"
			},
			args:   []string{},
			expErr: true,
		},
		"Invalid YAML fails": {
			setupConfig: func(t *testing.T) string {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "invalid.yaml")
				err := os.WriteFile(configPath, []byte("invalid: [yaml"), 0644)
				require.NoError(t, err)
				return configPath
			},
			args:   []string{},
			expErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Build the binary first
			buildCmd := exec.Command("go", "build", "-o", "sbx-test", "../../cmd/sbx")
			err := buildCmd.Run()
			require.NoError(t, err, "Failed to build sbx binary")
			defer os.Remove("sbx-test")

			// Setup temp DB
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			// Setup config
			configPath := tt.setupConfig(t)

			// If test name is "Duplicate name fails", create the first sandbox
			if strings.Contains(name, "Duplicate") {
				cmd := exec.Command("./sbx-test", "create", "-f", configPath, "--db-path", dbPath)
				err := cmd.Run()
				require.NoError(t, err)
			}

			// Build command args
			cmdArgs := []string{"create", "-f", configPath, "--db-path", dbPath, "--no-log"}
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
			}
		})
	}
}
