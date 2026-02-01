package firecracker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/slok/sbx/internal/model"
)

// Start starts a stopped Firecracker sandbox.
// Note: Firecracker doesn't support pause/resume. To "start" a stopped VM,
// we need to respawn the process. This is a limitation of Firecracker.
func (e *Engine) Start(ctx context.Context, id string) error {
	// For now, return an error explaining the limitation
	// A full implementation would:
	// 1. Re-read sandbox config from storage
	// 2. Re-spawn firecracker process
	// 3. Re-configure the VM
	// 4. Boot it
	return fmt.Errorf("firecracker VMs don't support restart after stop; use 'create' to start a new VM")
}

// Stop stops a running Firecracker sandbox.
func (e *Engine) Stop(ctx context.Context, id string) error {
	vmDir := e.VMDir(id)

	// Setup tasks if task repository is available
	if e.taskRepo != nil {
		taskNames := []string{"shutdown_vm", "kill_process"}
		if err := e.taskRepo.AddTasks(ctx, id, "stop", taskNames); err != nil {
			return fmt.Errorf("failed to add tasks: %w", err)
		}
	}

	// Task 1: Try graceful shutdown via SSH
	if err := e.executeTask(ctx, id, "stop", "shutdown_vm", func() error {
		e.logger.Infof("[1/2] Attempting graceful shutdown")
		return e.gracefulShutdown(ctx, id)
	}); err != nil {
		// Continue to kill process even if graceful shutdown fails
		e.logger.Warningf("Graceful shutdown failed: %v", err)
	}

	// Task 2: Kill the firecracker process
	if err := e.executeTask(ctx, id, "stop", "kill_process", func() error {
		e.logger.Infof("[2/2] Killing Firecracker process")
		return e.killFirecracker(vmDir)
	}); err != nil {
		return err
	}

	e.logger.Infof("Stopped Firecracker sandbox: %s", id)
	return nil
}

// Remove removes a Firecracker sandbox and all its resources.
func (e *Engine) Remove(ctx context.Context, id string) error {
	vmDir := e.VMDir(id)

	// We need the sandbox info to get TAP device and IPs for cleanup
	// For now, we'll use the hash-based allocation which is deterministic
	_, gateway, vmIP, tapDevice := e.allocateNetwork(id)

	// Setup tasks if task repository is available
	if e.taskRepo != nil {
		taskNames := []string{"kill_process", "cleanup_iptables", "delete_tap", "delete_files"}
		if err := e.taskRepo.AddTasks(ctx, id, "remove", taskNames); err != nil {
			return fmt.Errorf("failed to add tasks: %w", err)
		}
	}

	// Task 1: Kill firecracker process if running
	if err := e.executeTask(ctx, id, "remove", "kill_process", func() error {
		e.logger.Infof("[1/4] Killing Firecracker process")
		return e.killFirecracker(vmDir)
	}); err != nil {
		e.logger.Warningf("Could not kill process (may already be stopped): %v", err)
	}

	// Task 2: Cleanup iptables rules
	if err := e.executeTask(ctx, id, "remove", "cleanup_iptables", func() error {
		e.logger.Infof("[2/4] Cleaning up iptables rules")
		return e.cleanupIPTables(tapDevice, gateway, vmIP)
	}); err != nil {
		e.logger.Warningf("Could not cleanup iptables: %v", err)
	}

	// Task 3: Delete TAP device
	if err := e.executeTask(ctx, id, "remove", "delete_tap", func() error {
		e.logger.Infof("[3/4] Deleting TAP device: %s", tapDevice)
		return e.deleteTAP(tapDevice)
	}); err != nil {
		e.logger.Warningf("Could not delete TAP device: %v", err)
	}

	// Task 4: Delete VM files
	if err := e.executeTask(ctx, id, "remove", "delete_files", func() error {
		e.logger.Infof("[4/4] Deleting VM files")
		return os.RemoveAll(vmDir)
	}); err != nil {
		return fmt.Errorf("failed to delete VM files: %w", err)
	}

	e.logger.Infof("Removed Firecracker sandbox: %s", id)
	return nil
}

// Status returns the current status of a Firecracker sandbox.
func (e *Engine) Status(ctx context.Context, id string) (*model.Sandbox, error) {
	vmDir := e.VMDir(id)

	// Check if VM directory exists
	if _, err := os.Stat(vmDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("sandbox %s: %w", id, model.ErrNotFound)
	}

	// Read PID file
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		// No PID file means VM was never started or already cleaned up
		return &model.Sandbox{
			ID:     id,
			Status: model.SandboxStatusStopped,
		}, nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return nil, fmt.Errorf("invalid PID file: %w", err)
	}

	// Check if process is still running
	proc, err := os.FindProcess(pid)
	if err != nil {
		return &model.Sandbox{
			ID:     id,
			Status: model.SandboxStatusStopped,
			PID:    pid,
		}, nil
	}

	// Send signal 0 to check if process exists
	err = proc.Signal(syscall.Signal(0))
	status := model.SandboxStatusRunning
	if err != nil {
		status = model.SandboxStatusStopped
	}

	// Get network info from deterministic allocation
	_, _, vmIP, tapDevice := e.allocateNetwork(id)
	socketPath := filepath.Join(vmDir, "firecracker.sock")

	return &model.Sandbox{
		ID:         id,
		Status:     status,
		PID:        pid,
		SocketPath: socketPath,
		TapDevice:  tapDevice,
		InternalIP: vmIP,
	}, nil
}

// Exec executes a command inside a running Firecracker VM via SSH.
func (e *Engine) Exec(ctx context.Context, id string, command []string, opts model.ExecOpts) (*model.ExecResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command cannot be empty: %w", model.ErrNotValid)
	}

	// Get VM IP from deterministic allocation
	_, _, vmIP, _ := e.allocateNetwork(id)
	sshKeyPath := e.sshKeyManager.PrivateKeyPath()

	// Build SSH command
	args := []string{
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
	}

	// Add TTY if requested
	if opts.Tty {
		args = append(args, "-t", "-t") // Force TTY allocation
	}

	// Add target
	args = append(args, fmt.Sprintf("root@%s", vmIP))

	// Build command string
	var cmdStr string
	if opts.WorkingDir != "" {
		cmdStr = fmt.Sprintf("cd %s && %s", opts.WorkingDir, strings.Join(command, " "))
	} else {
		cmdStr = strings.Join(command, " ")
	}
	args = append(args, cmdStr)

	e.logger.Debugf("Executing SSH command: ssh %v", args)

	// Execute SSH
	cmd := exec.CommandContext(ctx, "ssh", args...)

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
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	return &model.ExecResult{
		ExitCode: exitCode,
	}, nil
}

// gracefulShutdown attempts to gracefully shutdown the VM via SSH.
func (e *Engine) gracefulShutdown(ctx context.Context, id string) error {
	_, _, vmIP, _ := e.allocateNetwork(id)
	sshKeyPath := e.sshKeyManager.PrivateKeyPath()

	// Try to run shutdown command
	return e.sshExec(ctx, vmIP, sshKeyPath, []string{"poweroff"})
}

// killFirecracker kills the firecracker process.
func (e *Engine) killFirecracker(vmDir string) error {
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No PID file, nothing to kill
		}
		return fmt.Errorf("could not read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return fmt.Errorf("invalid PID: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil // Process doesn't exist
	}

	// First try SIGTERM for graceful shutdown
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		// Process might not exist anymore
		return nil
	}

	// Give it a moment to terminate
	// In production, we'd wait with timeout and then SIGKILL
	// For simplicity, we'll also send SIGKILL
	_ = proc.Signal(syscall.SIGKILL)

	return nil
}
