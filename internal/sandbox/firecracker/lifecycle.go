package firecracker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"

	"github.com/slok/sbx/internal/conventions"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox"
	"github.com/slok/sbx/internal/ssh"
)

// Start starts a stopped Firecracker sandbox.
// Note: Firecracker doesn't support pause/resume. To "start" a stopped VM,
// we respawn the process transparently while preserving disk state.
// The user sees the same sandbox with all their disk changes intact.
func (e *Engine) Start(ctx context.Context, id string, opts sandbox.StartOpts) error {
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
	sb, err := e.repo.GetSandbox(ctx, id)
	if err != nil {
		return fmt.Errorf("could not get sandbox config: %w", err)
	}
	if sb.Config.FirecrackerEngine == nil {
		return fmt.Errorf("sandbox %s is not a firecracker sandbox", id)
	}

	// Network allocation is deterministic based on ID
	mac, gateway, vmIP, tapDevice := e.allocateNetwork(id)

	// Expand kernel path
	kernelPath := e.expandPath(sb.Config.FirecrackerEngine.KernelImage)
	if _, err := os.Stat(kernelPath); os.IsNotExist(err) {
		return fmt.Errorf("kernel image not found at %s", kernelPath)
	}

	socketPath := filepath.Join(vmDir, conventions.SocketFile)

	e.logger.Infof("Starting Firecracker sandbox: %s", id)
	e.logger.Debugf("Network: MAC=%s, Gateway=%s, VM IP=%s, TAP=%s", mac, gateway, vmIP, tapDevice)

	hasEgress := opts.EgressPolicy != nil
	totalSteps := 5
	if hasEgress {
		totalSteps = 6 // extra: egress setup (DNS patch + proxy + nftables)
	}
	step := 0
	nextStep := func() int { step++; return step }

	var startErr error
	var pid int

	// Task 1: Ensure networking resources exist (TAP + iptables)
	// If TAP is missing (e.g., after system reboot), recreate it
	e.logger.Debugf("[%d/%d] Ensuring network resources exist", nextStep(), totalSteps)
	if err := e.ensureNetworking(tapDevice, gateway, vmIP); err != nil {
		startErr = err
		goto cleanup
	}

	// Task: Spawn Firecracker process
	e.logger.Debugf("[%d/%d] Spawning Firecracker process", nextStep(), totalSteps)
	pid, err = e.spawnFirecracker(vmDir, socketPath)
	if err != nil {
		startErr = err
		goto cleanup
	}

	// Task: Configure VM via API (includes network config via kernel ip= parameter)
	e.logger.Debugf("[%d/%d] Configuring VM via Firecracker API", nextStep(), totalSteps)
	if err := e.configureVM(ctx, socketPath, kernelPath, vmDir, mac, tapDevice, vmIP, gateway, sb.Config.Resources); err != nil {
		startErr = err
		goto cleanup
	}

	// Task: Boot VM
	e.logger.Debugf("[%d/%d] Booting VM", nextStep(), totalSteps)
	if err := e.bootVM(ctx, socketPath); err != nil {
		startErr = err
		goto cleanup
	}

	// Task: Expand filesystem inside VM to fill resized disk
	e.logger.Debugf("[%d/%d] Expanding filesystem inside VM", nextStep(), totalSteps)
	if err := e.expandFilesystem(ctx, id, vmIP); err != nil {
		startErr = err
		goto cleanup
	}

	// Task (egress): Setup egress control after VM is running.
	// We patch DNS via SSH (post-boot) because the init system may overwrite
	// resolv.conf during boot. Then we set up nftables DNAT and spawn the proxy.
	if hasEgress {
		e.logger.Debugf("[%d/%d] Setting up egress control (DNS, nftables, proxy)", nextStep(), totalSteps)

		// Patch resolv.conf via SSH to point at gateway DNS forwarder.
		if err := e.patchDNSViaSSH(ctx, id, gateway); err != nil {
			startErr = fmt.Errorf("failed to patch DNS inside VM: %w", err)
			goto cleanup
		}

		// Spawn the egress proxy BEFORE setting up nftables DNAT, so the proxy
		// is ready to accept connections when traffic gets redirected.
		if err := e.spawnEgressProxy(vmDir, gateway, *opts.EgressPolicy); err != nil {
			startErr = fmt.Errorf("failed to spawn egress proxy: %w", err)
			goto cleanup
		}

		// Small delay to let the proxy bind its listen address.
		time.Sleep(500 * time.Millisecond)

		// Setup nftables DNAT rules to redirect traffic to the proxy.
		if err := e.setupEgressNftables(tapDevice, gateway, vmIP, conventions.EgressProxyPort, conventions.EgressDNSPort); err != nil {
			startErr = fmt.Errorf("failed to setup egress nftables: %w", err)
			goto cleanup
		}
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
		// Kill egress proxy if it was spawned.
		_ = e.killEgressProxy(vmDir)
		// Cleanup egress nftables if they were set up.
		if hasEgress {
			_ = e.cleanupEgressNftables(tapDevice)
		}
		return startErr
	}

	// Update sandbox with new PID and socket path
	sb.PID = pid
	sb.SocketPath = socketPath
	if err := e.repo.UpdateSandbox(ctx, *sb); err != nil {
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

	// Task 1: Try graceful shutdown via SSH
	e.logger.Debugf("[1/3] Attempting graceful shutdown")
	if err := e.gracefulShutdown(ctx, id); err != nil {
		// Continue to kill process even if graceful shutdown fails
		e.logger.Warningf("Graceful shutdown failed: %v", err)
	}

	// Task 2: Kill the egress proxy if running
	e.logger.Debugf("[2/3] Killing egress proxy (if running)")
	if err := e.killEgressProxy(vmDir); err != nil {
		e.logger.Warningf("Failed to kill egress proxy: %v", err)
	}

	// Task 3: Kill the firecracker process
	e.logger.Debugf("[3/3] Killing Firecracker process")
	if err := e.killFirecracker(vmDir); err != nil {
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

	// Task 1: Kill egress proxy if running
	e.logger.Debugf("[1/6] Killing egress proxy (if running)")
	if err := e.killEgressProxy(vmDir); err != nil {
		e.logger.Warningf("Could not kill egress proxy: %v", err)
	}

	// Task 2: Kill firecracker process if running
	e.logger.Debugf("[2/6] Killing Firecracker process")
	if err := e.killFirecracker(vmDir); err != nil {
		e.logger.Warningf("Could not kill process (may already be stopped): %v", err)
	}

	// Task 3: Cleanup egress nftables rules
	e.logger.Debugf("[3/6] Cleaning up egress nftables rules")
	if err := e.cleanupEgressNftables(tapDevice); err != nil {
		e.logger.Warningf("Could not cleanup egress nftables: %v", err)
	}

	// Task 4: Cleanup iptables rules
	e.logger.Debugf("[4/6] Cleaning up iptables rules")
	if err := e.cleanupIPTables(tapDevice, gateway, vmIP); err != nil {
		e.logger.Warningf("Could not cleanup iptables: %v", err)
	}

	// Task 5: Delete TAP device
	e.logger.Debugf("[5/6] Deleting TAP device: %s", tapDevice)
	if err := e.deleteTAP(tapDevice); err != nil {
		e.logger.Warningf("Could not delete TAP device: %v", err)
	}

	// Task 6: Delete VM files
	e.logger.Debugf("[6/6] Deleting VM files")
	if err := os.RemoveAll(vmDir); err != nil {
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
	pidPath := filepath.Join(vmDir, conventions.PIDFile)
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
	socketPath := filepath.Join(vmDir, conventions.SocketFile)

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
// For TTY mode (interactive shells), it shells out to the ssh binary.
// For non-TTY mode, it uses the pure Go SSH client.
func (e *Engine) Exec(ctx context.Context, id string, command []string, opts model.ExecOpts) (*model.ExecResult, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command cannot be empty: %w", model.ErrNotValid)
	}

	// Build the remote command string (shared by both TTY and non-TTY paths).
	cmdStr := buildRemoteCommand(command, opts)

	// TTY mode uses the ssh binary for proper terminal handling.
	if opts.Tty {
		return e.execWithTTY(ctx, id, cmdStr, opts)
	}

	// Non-TTY mode uses the pure Go SSH client.
	client, err := e.newSSHClient(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sandbox: %w", err)
	}
	defer client.Close()

	e.logger.Debugf("Executing SSH command (Go client): %s", cmdStr)

	exitCode, err := client.Exec(ctx, cmdStr, ssh.ExecOpts{
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	return &model.ExecResult{ExitCode: exitCode}, nil
}

// execWithTTY executes a command with TTY allocation using the ssh binary.
// This is needed for interactive shells where Go's SSH library would require
// manual terminal handling (raw mode, SIGWINCH, etc.).
func (e *Engine) execWithTTY(ctx context.Context, id, cmdStr string, opts model.ExecOpts) (*model.ExecResult, error) {
	_, _, vmIP, _ := e.allocateNetwork(id)
	sshKeyPath := e.sshKeyManager.PrivateKeyPath(id)

	args := []string{
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		"-t", "-t", // Force TTY allocation.
		fmt.Sprintf("root@%s", vmIP),
		cmdStr,
	}

	e.logger.Debugf("Executing SSH command (TTY): ssh %v", args)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	return &model.ExecResult{ExitCode: exitCode}, nil
}

// buildRemoteCommand builds the full remote command string from command parts and options.
// Handles: shell quoting, working directory, session env sourcing, environment variables.
func buildRemoteCommand(command []string, opts model.ExecOpts) string {
	quotedCommand := make([]string, 0, len(command))
	for _, part := range command {
		quotedCommand = append(quotedCommand, shellSingleQuote(part))
	}
	cmdStr := strings.Join(quotedCommand, " ")

	if opts.WorkingDir != "" {
		cmdStr = fmt.Sprintf("cd %s && %s", shellSingleQuote(opts.WorkingDir), cmdStr)
	}

	cmdStr = fmt.Sprintf("[ -f /etc/sbx/session-env.sh ] && . /etc/sbx/session-env.sh; %s", cmdStr)

	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for k := range opts.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		envParts := make([]string, 0, len(keys))
		for _, k := range keys {
			envParts = append(envParts, fmt.Sprintf("export %s=%s", k, shellSingleQuote(opts.Env[k])))
		}
		cmdStr = fmt.Sprintf("%s; %s", strings.Join(envParts, "; "), cmdStr)
	}

	return cmdStr
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// CopyTo copies a file or directory from the local host to the Firecracker VM via SFTP.
func (e *Engine) CopyTo(ctx context.Context, id string, srcLocal string, dstRemote string) error {
	client, err := e.newSSHClient(ctx, id)
	if err != nil {
		return fmt.Errorf("sandbox %s is not running or not reachable: %w: %w", id, err, model.ErrNotValid)
	}
	defer client.Close()

	e.logger.Debugf("Copying to VM %s: %s -> %s", id, srcLocal, dstRemote)

	if err := client.CopyTo(ctx, srcLocal, dstRemote); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source path '%s' does not exist: %w", srcLocal, model.ErrNotFound)
		}
		return fmt.Errorf("failed to copy to VM: %w", err)
	}

	e.logger.Debugf("Copied %s to %s:%s", srcLocal, id, dstRemote)
	return nil
}

// CopyFrom copies a file or directory from the Firecracker VM to the local host via SFTP.
func (e *Engine) CopyFrom(ctx context.Context, id string, srcRemote string, dstLocal string) error {
	client, err := e.newSSHClient(ctx, id)
	if err != nil {
		return fmt.Errorf("sandbox %s is not running or not reachable: %w: %w", id, err, model.ErrNotValid)
	}
	defer client.Close()

	e.logger.Debugf("Copying from VM %s: %s -> %s", id, srcRemote, dstLocal)

	if err := client.CopyFrom(ctx, srcRemote, dstLocal); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source path '%s' does not exist in sandbox: %w", srcRemote, model.ErrNotFound)
		}
		return fmt.Errorf("failed to copy from VM: %w", err)
	}

	e.logger.Debugf("Copied %s:%s to %s", id, srcRemote, dstLocal)
	return nil
}

// Forward forwards ports from localhost to the sandbox via SSH tunnel.
// Blocks until context is cancelled or connection drops.
func (e *Engine) Forward(ctx context.Context, id string, ports []model.PortMapping) error {
	if len(ports) == 0 {
		return fmt.Errorf("at least one port mapping is required: %w", model.ErrNotValid)
	}

	client, err := e.newSSHClient(ctx, id)
	if err != nil {
		return fmt.Errorf("SSH tunnel failed: %w", err)
	}
	defer client.Close()

	// Convert model.PortMapping to ssh.PortForward.
	portForwards := make([]ssh.PortForward, 0, len(ports))
	for _, pm := range ports {
		portForwards = append(portForwards, ssh.PortForward{
			LocalPort:  pm.LocalPort,
			RemotePort: pm.RemotePort,
		})
	}

	e.logger.Debugf("Starting SSH tunnel for %d ports", len(ports))

	return client.Forward(ctx, portForwards)
}

// gracefulShutdown attempts to gracefully shutdown the VM via SSH.
func (e *Engine) gracefulShutdown(ctx context.Context, id string) error {
	return e.sshExec(ctx, id, "poweroff")
}

// patchDNSViaSSH overwrites /etc/resolv.conf inside the running VM via SSH.
// This is done post-boot because the init system may overwrite the file during boot.
// Handles symlinks by removing the symlink first, then writing a regular file.
func (e *Engine) patchDNSViaSSH(ctx context.Context, sandboxID, gatewayIP string) error {
	// Remove any symlink (e.g., /etc/resolv.conf -> /run/systemd/resolve/resolv.conf),
	// then write the resolv.conf as a regular file.
	cmd := fmt.Sprintf("rm -f /etc/resolv.conf && printf 'nameserver %s\\n' > /etc/resolv.conf", gatewayIP)
	return e.sshExec(ctx, sandboxID, cmd)
}

// spawnEgressProxy forks a `sbx egress-proxy` process and writes its PID file.
// The proxy is spawned after the VM boots so traffic can be redirected immediately.
func (e *Engine) spawnEgressProxy(vmDir, gateway string, policy model.EgressPolicy) error {
	// Use the configured sbx binary, falling back to os.Executable().
	// The explicit path is needed when the engine runs inside a test binary
	// or other non-sbx process that doesn't have the egress-proxy subcommand.
	selfBin := e.sbxBinary
	if selfBin == "" {
		var err error
		selfBin, err = os.Executable()
		if err != nil {
			return fmt.Errorf("could not find own binary: %w", err)
		}
	}

	// Marshal policy to JSON.
	policyJSON, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("could not marshal egress policy: %w", err)
	}

	listenAddr := fmt.Sprintf("%s:%d", gateway, conventions.EgressProxyPort)
	dnsAddr := fmt.Sprintf("%s:%d", gateway, conventions.EgressDNSPort)

	args := []string{
		"egress-proxy",
		"--listen", listenAddr,
		"--dns", dnsAddr,
		"--policy", string(policyJSON),
	}

	cmd := exec.Command(selfBin, args...)

	// Redirect stderr to log file.
	logPath := filepath.Join(vmDir, conventions.EgressProxyLogFile)
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("could not create egress proxy log: %w", err)
	}
	cmd.Stderr = logFile
	cmd.Stdout = logFile

	// Detach from parent process group so the proxy survives CLI exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("could not start egress proxy: %w", err)
	}
	logFile.Close()

	// Write PID file.
	pidPath := filepath.Join(vmDir, conventions.EgressProxyPIDFile)
	pidContent := fmt.Sprintf("%d", cmd.Process.Pid)
	if err := os.WriteFile(pidPath, []byte(pidContent), 0644); err != nil {
		// Kill the process we just started since we can't track it.
		_ = cmd.Process.Kill()
		return fmt.Errorf("could not write egress proxy PID file: %w", err)
	}

	// Release the process so it doesn't become a zombie.
	_ = cmd.Process.Release()

	e.logger.Debugf("Spawned egress proxy (PID %d) at %s", cmd.Process.Pid, listenAddr)
	return nil
}

// killEgressProxy kills the egress proxy process if its PID file exists.
func (e *Engine) killEgressProxy(vmDir string) error {
	pidPath := filepath.Join(vmDir, conventions.EgressProxyPIDFile)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No PID file, no proxy to kill.
		}
		return fmt.Errorf("could not read egress proxy PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return fmt.Errorf("invalid egress proxy PID: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		// Process might not exist anymore.
		return nil
	}

	_ = proc.Signal(syscall.SIGKILL)

	// Clean up PID file.
	_ = os.Remove(pidPath)

	e.logger.Debugf("Killed egress proxy (PID %d)", pid)
	return nil
}

// killFirecracker kills the firecracker process.
func (e *Engine) killFirecracker(vmDir string) error {
	pidPath := filepath.Join(vmDir, conventions.PIDFile)
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
