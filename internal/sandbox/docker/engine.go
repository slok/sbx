package docker

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/oklog/ulid/v2"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
)

// DockerClient is the interface for Docker operations that we use.
// This allows us to mock the Docker client for testing.
type DockerClient interface {
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
}

// EngineConfig is the configuration for the Docker engine.
type EngineConfig struct {
	Client   DockerClient
	TaskRepo storage.TaskRepository
	Logger   log.Logger
}

func (c *EngineConfig) defaults() error {
	if c.Client == nil {
		// Create a default Docker client
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("could not create Docker client: %w", err)
		}
		c.Client = cli
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "engine.Docker"})
	return nil
}

// Engine is the Docker implementation of the sandbox.Engine interface.
type Engine struct {
	client   DockerClient
	taskRepo storage.TaskRepository
	logger   log.Logger
}

// NewEngine creates a new Docker engine.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Engine{
		client:   cfg.Client,
		taskRepo: cfg.TaskRepo,
		logger:   cfg.Logger,
	}, nil
}

// Create creates and starts a new Docker container sandbox.
func (e *Engine) Create(ctx context.Context, cfg model.SandboxConfig) (*model.Sandbox, error) {
	// Validate that we have Docker engine config
	if cfg.DockerEngine == nil {
		return nil, fmt.Errorf("docker engine configuration is required")
	}

	// Generate ULID for sandbox
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	containerName := fmt.Sprintf("sbx-%s", strings.ToLower(id))

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		taskNames := []string{"pull_image", "create_container", "start_container"}
		if err := e.taskRepo.AddTasks(ctx, id, "create", taskNames); err != nil {
			return nil, fmt.Errorf("failed to add tasks: %w", err)
		}
	}

	// Execute tasks
	var containerID string
	var createErr error

	// Task 1: Pull the image
	if err := e.executeTask(ctx, id, "create", "pull_image", func() error {
		e.logger.Infof("[1/3] Pulling image: %s", cfg.DockerEngine.Image)
		pullResp, err := e.client.ImagePull(ctx, cfg.DockerEngine.Image, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %w", cfg.DockerEngine.Image, err)
		}
		// Consume the pull response to ensure it completes
		_, _ = io.Copy(io.Discard, pullResp)
		pullResp.Close()
		return nil
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 2: Create container
	if err := e.executeTask(ctx, id, "create", "create_container", func() error {
		e.logger.Infof("[2/3] Creating container: %s", containerName)

		// Prepare environment variables
		var envVars []string
		for k, v := range cfg.Env {
			envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
		}

		// Create container config
		containerConfig := &container.Config{
			Image: cfg.DockerEngine.Image,
			Env:   envVars,
			Cmd:   []string{"tail", "-f", "/dev/null"}, // Keep container running
		}

		// Create host config with resource limits
		hostConfig := &container.HostConfig{
			Resources: container.Resources{
				NanoCPUs: int64(cfg.Resources.VCPUs * 1e9),            // Convert VCPUs to nano CPUs
				Memory:   int64(cfg.Resources.MemoryMB * 1024 * 1024), // Convert MB to bytes
			},
		}

		// Create the container
		resp, err := e.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
		if err != nil {
			return fmt.Errorf("failed to create container: %w", err)
		}

		containerID = resp.ID
		return nil
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 3: Start the container
	if err := e.executeTask(ctx, id, "create", "start_container", func() error {
		e.logger.Infof("[3/3] Starting container: %s", containerID)
		if err := e.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		return nil
	}); err != nil {
		createErr = err
		goto cleanup
	}

cleanup:
	// If we have an error, return it
	if createErr != nil {
		return nil, createErr
	}

	// Create sandbox model
	now := time.Now().UTC()
	sandbox := &model.Sandbox{
		ID:          id,
		Name:        cfg.Name,
		Status:      model.SandboxStatusRunning,
		Config:      cfg,
		ContainerID: containerID,
		CreatedAt:   now,
		StartedAt:   &now,
	}

	e.logger.Infof("Created Docker sandbox: %s (container: %s)", id, containerID)

	return sandbox, nil
}

// Exec executes a command inside a running Docker container sandbox.
func (e *Engine) Exec(ctx context.Context, id string, command []string, opts model.ExecOpts) (*model.ExecResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command cannot be empty: %w", model.ErrNotValid)
	}

	containerName := fmt.Sprintf("sbx-%s", strings.ToLower(id))

	// Build docker exec command
	args := []string{"exec"}

	// Add flags
	if opts.Tty {
		args = append(args, "-it")
	}
	if opts.WorkingDir != "" {
		args = append(args, "-w", opts.WorkingDir)
	}
	for k, v := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add container name and command
	args = append(args, containerName)
	args = append(args, command...)

	// Execute docker command
	e.logger.Debugf("Executing command in container %s: docker %v", containerName, args)

	cmd := exec.CommandContext(ctx, "docker", args...)

	// Wire up streams
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}

	// Run the command
	err := cmd.Run()

	// Get exit code
	exitCode := 0
	if err != nil {
		// Check if it's an exit error (non-zero exit code)
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			e.logger.Debugf("Command exited with code %d", exitCode)
		} else {
			// Other error (e.g., container not found, not running)
			if strings.Contains(err.Error(), "No such container") {
				return nil, fmt.Errorf("container %s: %w", containerName, model.ErrNotFound)
			}
			if strings.Contains(err.Error(), "is not running") {
				return nil, fmt.Errorf("container %s is not running: %w", containerName, model.ErrNotValid)
			}
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	return &model.ExecResult{
		ExitCode: exitCode,
	}, nil
}

// executeTask executes a task function and tracks its completion.
func (e *Engine) executeTask(ctx context.Context, sandboxID, operation, taskName string, fn func() error) error {
	// If no task manager, just execute the function
	if e.taskRepo == nil {
		return fn()
	}

	// Get the next task - should be the one with this name
	tsk, err := e.taskRepo.NextTask(ctx, sandboxID, operation)
	if err != nil {
		return fmt.Errorf("failed to get next task: %w", err)
	}
	if tsk == nil {
		return fmt.Errorf("no pending task found for operation %s", operation)
	}
	if tsk.Name != taskName {
		return fmt.Errorf("expected task %s, got %s", taskName, tsk.Name)
	}

	// Execute the task function
	err = fn()
	if err != nil {
		// Mark task as failed
		if failErr := e.taskRepo.FailTask(ctx, tsk.ID, err); failErr != nil {
			e.logger.Errorf("Failed to mark task as failed: %v", failErr)
		}
		return err
	}

	// Mark task as completed
	if err := e.taskRepo.CompleteTask(ctx, tsk.ID); err != nil {
		return fmt.Errorf("failed to mark task as completed: %w", err)
	}

	return nil
}

// Start starts a stopped Docker container sandbox.
func (e *Engine) Start(ctx context.Context, id string) error {
	containerName := fmt.Sprintf("sbx-%s", strings.ToLower(id))

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		if err := e.taskRepo.AddTask(ctx, id, "start", "start_container"); err != nil {
			return fmt.Errorf("failed to add task: %w", err)
		}
	}

	// Task: Start container
	if err := e.executeTask(ctx, id, "start", "start_container", func() error {
		e.logger.Infof("[1/1] Starting container: %s", containerName)
		if err := e.client.ContainerStart(ctx, containerName, container.StartOptions{}); err != nil {
			// Check if already running - this is idempotent
			if strings.Contains(err.Error(), "already started") || strings.Contains(err.Error(), "is already running") {
				e.logger.Debugf("Container %s is already running", containerName)
				return nil
			}
			return fmt.Errorf("failed to start container %s: %w", containerName, err)
		}
		return nil
	}); err != nil {
		return err
	}

	e.logger.Infof("Started Docker sandbox: %s", id)
	return nil
}

// Stop stops a running Docker container sandbox.
func (e *Engine) Stop(ctx context.Context, id string) error {
	containerName := fmt.Sprintf("sbx-%s", strings.ToLower(id))

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		if err := e.taskRepo.AddTask(ctx, id, "stop", "stop_container"); err != nil {
			return fmt.Errorf("failed to add task: %w", err)
		}
	}

	// Task: Stop container
	if err := e.executeTask(ctx, id, "stop", "stop_container", func() error {
		e.logger.Infof("[1/1] Stopping container: %s", containerName)
		timeout := 10 // 10 seconds timeout for graceful shutdown
		if err := e.client.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout}); err != nil {
			// Check if already stopped - this is idempotent
			if strings.Contains(err.Error(), "is already stopped") || strings.Contains(err.Error(), "is not running") {
				e.logger.Debugf("Container %s is already stopped", containerName)
				return nil
			}
			return fmt.Errorf("failed to stop container %s: %w", containerName, err)
		}
		return nil
	}); err != nil {
		return err
	}

	e.logger.Infof("Stopped Docker sandbox: %s", id)
	return nil
}

// Remove removes a Docker container sandbox.
func (e *Engine) Remove(ctx context.Context, id string) error {
	containerName := fmt.Sprintf("sbx-%s", strings.ToLower(id))

	// Setup tasks if task manager is available
	if e.taskRepo != nil {
		if err := e.taskRepo.AddTask(ctx, id, "remove", "remove_container"); err != nil {
			return fmt.Errorf("failed to add task: %w", err)
		}
	}

	// Task: Remove container
	if err := e.executeTask(ctx, id, "remove", "remove_container", func() error {
		e.logger.Infof("[1/1] Removing container: %s", containerName)
		if err := e.client.ContainerRemove(ctx, containerName, container.RemoveOptions{
			Force: true, // Force removal even if running
		}); err != nil {
			// Check if already removed
			if strings.Contains(err.Error(), "No such container") {
				e.logger.Debugf("Container %s already removed", containerName)
				return nil
			}
			return fmt.Errorf("failed to remove container %s: %w", containerName, err)
		}
		return nil
	}); err != nil {
		return err
	}

	e.logger.Infof("Removed Docker sandbox: %s", id)
	return nil
}

// Status returns the current status of a Docker container sandbox.
func (e *Engine) Status(ctx context.Context, id string) (*model.Sandbox, error) {
	containerName := fmt.Sprintf("sbx-%s", strings.ToLower(id))

	e.logger.Debugf("Inspecting container: %s", containerName)
	info, err := e.client.ContainerInspect(ctx, containerName)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			return nil, fmt.Errorf("container %s: %w", containerName, model.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerName, err)
	}

	// Map Docker state to SandboxStatus
	var status model.SandboxStatus
	switch info.State.Status {
	case "created":
		status = model.SandboxStatusPending
	case "running":
		status = model.SandboxStatusRunning
	case "exited":
		status = model.SandboxStatusStopped
	case "dead":
		status = model.SandboxStatusFailed
	default:
		status = model.SandboxStatusFailed
	}

	// Parse timestamps
	createdAt, _ := time.Parse(time.RFC3339Nano, info.Created)
	var startedAt, stoppedAt *time.Time
	if info.State.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, info.State.StartedAt); err == nil {
			startedAt = &t
		}
	}
	if info.State.FinishedAt != "" && info.State.Status == "exited" {
		if t, err := time.Parse(time.RFC3339Nano, info.State.FinishedAt); err == nil {
			stoppedAt = &t
		}
	}

	// Build sandbox model
	// Note: We can't fully reconstruct the original config from container inspect
	// This is a limitation - ideally we'd store this in labels or retrieve from storage
	sandbox := &model.Sandbox{
		ID:          id,
		Name:        strings.TrimPrefix(info.Name, "/"), // Docker prefixes with /
		Status:      status,
		ContainerID: info.ID,
		CreatedAt:   createdAt,
		StartedAt:   startedAt,
		StoppedAt:   stoppedAt,
		Error:       info.State.Error,
	}

	return sandbox, nil
}
