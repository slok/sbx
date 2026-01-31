package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/require"
)

// dockerHelper provides utilities for interacting with Docker in tests.
type dockerHelper struct {
	client *client.Client
}

// newDockerHelper creates a new Docker helper for tests.
func newDockerHelper(t *testing.T) *dockerHelper {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "Failed to create Docker client")

	return &dockerHelper{client: cli}
}

// containerExists checks if a container with the given name exists.
func (d *dockerHelper) containerExists(t *testing.T, containerName string) bool {
	ctx := context.Background()
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	require.NoError(t, err, "Failed to list containers")

	for _, c := range containers {
		for _, name := range c.Names {
			// Docker names start with /
			if name == "/"+containerName || name == containerName {
				return true
			}
		}
	}
	return false
}

// getContainerStatus returns the status of a container (running, exited, etc).
func (d *dockerHelper) getContainerStatus(t *testing.T, containerName string) string {
	ctx := context.Background()
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	require.NoError(t, err, "Failed to list containers")

	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName || name == containerName {
				return c.State
			}
		}
	}
	return ""
}

// requireContainerExists asserts that a container exists.
func (d *dockerHelper) requireContainerExists(t *testing.T, containerName string) {
	require.True(t, d.containerExists(t, containerName),
		"Expected container %s to exist", containerName)
}

// requireContainerNotExists asserts that a container does not exist.
func (d *dockerHelper) requireContainerNotExists(t *testing.T, containerName string) {
	require.False(t, d.containerExists(t, containerName),
		"Expected container %s to not exist", containerName)
}

// requireContainerRunning asserts that a container is running.
func (d *dockerHelper) requireContainerRunning(t *testing.T, containerName string) {
	status := d.getContainerStatus(t, containerName)
	require.Equal(t, "running", status,
		"Expected container %s to be running, got status: %s", containerName, status)
}

// requireContainerStopped asserts that a container is not running.
func (d *dockerHelper) requireContainerStopped(t *testing.T, containerName string) {
	status := d.getContainerStatus(t, containerName)
	require.NotEqual(t, "running", status,
		"Expected container %s to be stopped, got status: %s", containerName, status)
}

// waitForContainerStopped waits for a container to stop (up to 15 seconds).
func (d *dockerHelper) waitForContainerStopped(t *testing.T, containerName string) {
	// Poll for up to 15 seconds (docker stop has 10 second timeout)
	for i := 0; i < 30; i++ {
		status := d.getContainerStatus(t, containerName)
		if status != "running" {
			return
		}
		// Wait 500ms between checks
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("Container %s did not stop within 15 seconds", containerName)
}

// cleanupContainer removes a container if it exists (for test cleanup).
func (d *dockerHelper) cleanupContainer(t *testing.T, containerName string) {
	ctx := context.Background()
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		t.Logf("Warning: Failed to list containers during cleanup: %v", err)
		return
	}

	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName || name == containerName {
				if err := d.client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
					t.Logf("Warning: Failed to remove container %s during cleanup: %v", containerName, err)
				} else {
					t.Logf("Cleaned up container: %s", containerName)
				}
				return
			}
		}
	}
}

// cleanupAllSbxContainers removes all containers with names starting with "sbx-".
func (d *dockerHelper) cleanupAllSbxContainers(t *testing.T) {
	ctx := context.Background()
	containers, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		t.Logf("Warning: Failed to list containers during cleanup: %v", err)
		return
	}

	cleaned := 0
	for _, c := range containers {
		for _, name := range c.Names {
			// Check if this is an sbx container
			if len(name) > 5 && (name[:5] == "/sbx-" || name[:4] == "sbx-") {
				if err := d.client.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
					t.Logf("Warning: Failed to remove container %s during cleanup: %v", name, err)
				} else {
					cleaned++
				}
				break
			}
		}
	}

	if cleaned > 0 {
		t.Logf("Cleaned up %d sbx containers", cleaned)
	}
}

// getContainerName returns the expected container name for a sandbox ID.
func getContainerName(sandboxID string) string {
	// Docker container names are lowercase, but IDs might be uppercase
	return fmt.Sprintf("sbx-%s", strings.ToLower(sandboxID))
}
