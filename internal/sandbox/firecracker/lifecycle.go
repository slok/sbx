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

	"github.com/vishvananda/netlink"

	"github.com/slok/sbx/internal/model"
)

// Start starts a stopped Firecracker sandbox.
// Note: Firecracker doesn't support pause/resume. To "start" a stopped VM,
// we respawn the process transparently while preserving disk state.
// The user sees the same sandbox with all their disk changes intact.
func (e *Engine) Start(ctx context.Context, id string) error {
	vmDir := e.VMDir(id)

	// Validate VM directory exists
	if _, err := os.Stat(vmDir); os.IsNotExist(err) {
		return fmt.Errorf("sandbox %s: VM directory not found: %w", id, model.ErrNotFound)
	}

	// Validate rootfs exists (contains user's disk state)
	rootfsPath := e.RootFSPath(vmDir)
	if _, err := os.Stat(rootfsPath); os.IsNotExist(err) {
		return fmt.Errorf("sandbox %s: rootfs not found at %s - sandbox needs to be recreated", id, rootfsPath)
	}

	// Get sandbox config from repository
	if e.repo == nil {
		return fmt.Errorf("cannot start firecracker sandbox: repository not configured")
	}
	sandbox, err := e.repo.GetSandbox(ctx, id)
	if err != nil {
		return fmt.Errorf("could not get sandbox config: %w", err)
	}
	if sandbox.Config.FirecrackerEngine == nil {
		return fmt.Errorf("sandbox %s is not a firecracker sandbox", id)
	}

	// Network allocation is deterministic based on ID
	mac, gateway, vmIP, tapDevice := e.allocateNetwork(id)

	// Expand kernel path
	kernelPath := e.expandPath(sandbox.Config.FirecrackerEngine.KernelImage)
	if _, err := os.Stat(kernelPath); os.IsNotExist(err) {
		return fmt.Errorf("kernel image not found at %s", kernelPath)
	}

	socketPath := filepath.Join(vmDir, "firecracker.sock")

	e.logger.Infof("Starting Firecracker sandbox: %s", id)
	e.logger.Debugf("Network: MAC=%s, Gateway=%s, VM IP=%s, TAP=%s", mac, gateway, vmIP, tapDevice)

	// Setup tasks if task repository is available
	if e.taskRepo != nil {
		taskNames := []string{
			"ensure_networking",
			"spawn_firecracker",
			"configure_vm",
			"boot_vm",
			"wait_ssh",
		}
		if err := e.taskRepo.AddTasks(ctx, id, "start", taskNames); err != nil {
			return fmt.Errorf("failed to add tasks: %w", err)
		}
	}

	var startErr error
	var pid int

	// Task 1: Ensure networking resources exist (TAP + iptables)
	// If TAP is missing (e.g., after system reboot), recreate it
	if err := e.executeTask(ctx, id, "start", "ensure_networking", func() error {
		e.logger.Infof("[1/5] Ensuring network resources exist")
		return e.ensureNetworking(tapDevice, gateway, vmIP)
	}); err != nil {
		startErr = err
		goto cleanup
	}

	// Task 2: Spawn Firecracker process
	if err := e.executeTask(ctx, id, "start", "spawn_firecracker", func() error {
		e.logger.Infof("[2/5] Spawning Firecracker process")
		var err error
		pid, err = e.spawnFirecracker(vmDir, socketPath)
		return err
	}); err != nil {
		startErr = err
		goto cleanup
	}

	// Task 3: Configure VM via API
	if err := e.executeTask(ctx, id, "start", "configure_vm", func() error {
		e.logger.Infof("[3/5] Configuring VM via Firecracker API")
		return e.configureVM(ctx, socketPath, kernelPath, vmDir, mac, tapDevice, sandbox.Config.Resources)
	}); err != nil {
		startErr = err
		goto cleanup
	}

	// Task 4: Boot VM
	if err := e.executeTask(ctx, id, "start", "boot_vm", func() error {
		e.logger.Infof("[4/5] Booting VM")
		return e.bootVM(ctx, socketPath)
	}); err != nil {
		startErr = err
		goto cleanup
	}

	// Task 5: Wait for SSH to be available
	if err := e.executeTask(ctx, id, "start", "wait_ssh", func() error {
		e.logger.Infof("[5/5] Waiting for SSH to become available")
		sshKeyPath := e.sshKeyManager.PrivateKeyPath()
		return e.waitForSSH(ctx, vmIP, sshKeyPath)
	}); err != nil {
		startErr = err
		goto cleanup
	}

cleanup:
	if startErr != nil {
		e.logger.Errorf("Start failed: %v", startErr)
		// Kill firecracker process if it was started
		if pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Kill()
			}
		}
		return startErr
	}

	// Update sandbox with new PID and socket path
	sandbox.PID = pid
	sandbox.SocketPath = socketPath
	if err := e.repo.UpdateSandbox(ctx, *sandbox); err != nil {
		e.logger.Warningf("Failed to update sandbox PID in repository: %v", err)
		// Don't fail the start - VM is running, just log the warning
	}

	e.logger.Infof("Started Firecracker sandbox: %s (PID: %d, IP: %s)", id, pid, vmIP)
	return nil
}

// ensureNetworking ensures TAP device and iptables rules exist.
// Creates them if missing (e.g., after system reboot).
func (e *Engine) ensureNetworking(tapDevice, gateway, vmIP string) error {
	// Check if TAP device exists
	_, err := netlink.LinkByName(tapDevice)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such") {
			// TAP doesn't exist, create it
			e.logger.Infof("TAP device %s missing, recreating", tapDevice)
			if err := e.createTAP(tapDevice, gateway); err != nil {
				return fmt.Errorf("failed to recreate TAP device: %w", err)
			}
			// Also need to recreate iptables rules
			if err := e.setupIPTables(tapDevice, gateway, vmIP); err != nil {
				return fmt.Errorf("failed to recreate iptables rules: %w", err)
			}
		} else {
			return fmt.Errorf("failed to check TAP device: %w", err)
		}
	}
	// TAP exists, assume iptables rules are also in place
	// (if they were removed, user can rm and recreate the sandbox)
	return nil
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
