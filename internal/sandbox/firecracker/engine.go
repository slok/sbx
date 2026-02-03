package firecracker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/ssh"
	"github.com/slok/sbx/internal/storage"
)

const (
	// DefaultDataDir is the default directory for sbx data.
	DefaultDataDir = ".sbx"
	// VMsDir is the subdirectory for VM data.
	VMsDir = "vms"
	// ImagesDir is the subdirectory for kernel and rootfs images.
	ImagesDir = "images"
)

// EngineConfig is the configuration for the Firecracker engine.
type EngineConfig struct {
	// DataDir is the base directory for sbx data (default: ~/.sbx).
	DataDir string
	// FirecrackerBinary is the path to the firecracker binary.
	// If empty, it will be looked up in PATH and ./bin.
	FirecrackerBinary string
	// Repository is the sandbox storage repository (required for Start to read sandbox config).
	Repository storage.Repository
	// TaskRepo is the repository for task tracking.
	TaskRepo storage.TaskRepository
	// Logger for logging.
	Logger log.Logger
}

func (c *EngineConfig) defaults() error {
	if c.DataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not get user home dir: %w", err)
		}
		c.DataDir = filepath.Join(home, DefaultDataDir)
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	c.Logger = c.Logger.WithValues(log.Kv{"svc": "engine.Firecracker"})
	return nil
}

// Engine is the Firecracker implementation of the sandbox.Engine interface.
type Engine struct {
	dataDir           string
	firecrackerBinary string
	repo              storage.Repository
	taskRepo          storage.TaskRepository
	sshKeyManager     *ssh.KeyManager
	logger            log.Logger
}

// NewEngine creates a new Firecracker engine.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create SSH key manager
	sshKeyDir := filepath.Join(cfg.DataDir, "ssh")
	sshKeyManager := ssh.NewKeyManager(sshKeyDir)

	return &Engine{
		dataDir:           cfg.DataDir,
		firecrackerBinary: cfg.FirecrackerBinary,
		repo:              cfg.Repository,
		taskRepo:          cfg.TaskRepo,
		sshKeyManager:     sshKeyManager,
		logger:            cfg.Logger,
	}, nil
}

// VMDir returns the directory for a specific VM.
func (e *Engine) VMDir(sandboxID string) string {
	return filepath.Join(e.dataDir, VMsDir, sandboxID)
}

// ImagesPath returns the path to the images directory.
func (e *Engine) ImagesPath() string {
	return filepath.Join(e.dataDir, ImagesDir)
}

// SSHKeyManager returns the SSH key manager.
func (e *Engine) SSHKeyManager() *ssh.KeyManager {
	return e.sshKeyManager
}

// Check performs preflight checks for the Firecracker engine.
func (e *Engine) Check(ctx context.Context) []model.CheckResult {
	var results []model.CheckResult

	// Check 1: KVM available
	results = append(results, e.checkKVM())

	// Check 2: Firecracker binary
	results = append(results, e.checkFirecrackerBinary())

	// Check 3: IP forwarding
	results = append(results, e.checkIPForward())

	// Check 4: iptables available
	results = append(results, e.checkIPTables())

	return results
}

// checkKVM checks if /dev/kvm is available and writable.
func (e *Engine) checkKVM() model.CheckResult {
	kvmPath := "/dev/kvm"

	info, err := os.Stat(kvmPath)
	if err != nil {
		if os.IsNotExist(err) {
			return model.CheckResult{
				ID:      "kvm_available",
				Message: "/dev/kvm does not exist (KVM not available)",
				Status:  model.CheckStatusError,
			}
		}
		return model.CheckResult{
			ID:      "kvm_available",
			Message: fmt.Sprintf("Cannot access /dev/kvm: %v", err),
			Status:  model.CheckStatusError,
		}
	}

	// Check if it's a device (character device)
	if info.Mode()&os.ModeCharDevice == 0 {
		return model.CheckResult{
			ID:      "kvm_available",
			Message: "/dev/kvm is not a character device",
			Status:  model.CheckStatusError,
		}
	}

	// Check if writable by trying to open it
	f, err := os.OpenFile(kvmPath, os.O_RDWR, 0)
	if err != nil {
		return model.CheckResult{
			ID:      "kvm_available",
			Message: fmt.Sprintf("No write permission to /dev/kvm: %v", err),
			Status:  model.CheckStatusError,
		}
	}
	f.Close()

	return model.CheckResult{
		ID:      "kvm_available",
		Message: "KVM is available and writable",
		Status:  model.CheckStatusOK,
	}
}

// checkFirecrackerBinary checks if firecracker binary is available.
func (e *Engine) checkFirecrackerBinary() model.CheckResult {
	// Check paths in order of priority
	paths := []string{}

	// 1. Explicit path from config
	if e.firecrackerBinary != "" {
		paths = append(paths, e.firecrackerBinary)
	}

	// 2. Look in ./bin directory (relative to current working dir)
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "bin", "firecracker"))
	}

	// 3. Look in PATH
	if p, err := exec.LookPath("firecracker"); err == nil {
		paths = append(paths, p)
	}

	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			// Check if executable
			if info.Mode()&0111 != 0 {
				// Try to get version
				cmd := exec.Command(path, "--version")
				out, err := cmd.Output()
				version := "unknown"
				if err == nil {
					version = strings.TrimSpace(string(out))
				}
				return model.CheckResult{
					ID:      "firecracker_binary",
					Message: fmt.Sprintf("Firecracker found at %s (%s)", path, version),
					Status:  model.CheckStatusOK,
				}
			}
		}
	}

	return model.CheckResult{
		ID:      "firecracker_binary",
		Message: "Firecracker binary not found in PATH or ./bin",
		Status:  model.CheckStatusError,
	}
}

// checkIPForward checks if IP forwarding is enabled.
func (e *Engine) checkIPForward() model.CheckResult {
	data, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return model.CheckResult{
			ID:      "ip_forward",
			Message: fmt.Sprintf("Cannot read IP forwarding status: %v", err),
			Status:  model.CheckStatusWarning,
		}
	}

	if strings.TrimSpace(string(data)) == "1" {
		return model.CheckResult{
			ID:      "ip_forward",
			Message: "IP forwarding is enabled",
			Status:  model.CheckStatusOK,
		}
	}

	return model.CheckResult{
		ID:      "ip_forward",
		Message: "IP forwarding is disabled (may affect networking)",
		Status:  model.CheckStatusWarning,
	}
}

// checkIPTables checks if iptables is available.
func (e *Engine) checkIPTables() model.CheckResult {
	path, err := exec.LookPath("iptables")
	if err != nil {
		return model.CheckResult{
			ID:      "iptables",
			Message: "iptables not found in PATH",
			Status:  model.CheckStatusError,
		}
	}

	return model.CheckResult{
		ID:      "iptables",
		Message: fmt.Sprintf("iptables found at %s", path),
		Status:  model.CheckStatusOK,
	}
}

// CheckKernelImage checks if a kernel image exists at the given path.
func (e *Engine) CheckKernelImage(kernelPath string) model.CheckResult {
	path := e.expandPath(kernelPath)
	if _, err := os.Stat(path); err != nil {
		return model.CheckResult{
			ID:      "kernel_image",
			Message: fmt.Sprintf("Kernel image not found at %s", kernelPath),
			Status:  model.CheckStatusError,
		}
	}
	return model.CheckResult{
		ID:      "kernel_image",
		Message: fmt.Sprintf("Kernel image found at %s", kernelPath),
		Status:  model.CheckStatusOK,
	}
}

// CheckRootFS checks if a rootfs image exists at the given path.
func (e *Engine) CheckRootFS(rootfsPath string) model.CheckResult {
	path := e.expandPath(rootfsPath)
	if _, err := os.Stat(path); err != nil {
		return model.CheckResult{
			ID:      "base_rootfs",
			Message: fmt.Sprintf("Root filesystem not found at %s", rootfsPath),
			Status:  model.CheckStatusError,
		}
	}
	return model.CheckResult{
		ID:      "base_rootfs",
		Message: fmt.Sprintf("Root filesystem found at %s", rootfsPath),
		Status:  model.CheckStatusOK,
	}
}

// expandPath expands ~ to user home directory.
func (e *Engine) expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// allocateNetwork allocates IP/MAC/TAP based on sandbox ID using hash-based allocation.
// Returns: MAC address, gateway IP, VM IP, TAP device name.
func (e *Engine) allocateNetwork(sandboxID string) (mac, gateway, vmIP, tapDevice string) {
	hash := sha256.Sum256([]byte(sandboxID))
	xx, yy := hash[0], hash[1]

	mac = fmt.Sprintf("06:00:0A:%02X:%02X:02", xx, yy)
	gateway = fmt.Sprintf("10.%d.%d.1", xx, yy)
	vmIP = fmt.Sprintf("10.%d.%d.2", xx, yy)
	tapDevice = fmt.Sprintf("sbx-%02x%02x", xx, yy)

	return mac, gateway, vmIP, tapDevice
}

// Create creates a new Firecracker microVM sandbox.
func (e *Engine) Create(ctx context.Context, cfg model.SandboxConfig) (*model.Sandbox, error) {
	// Validate that we have Firecracker engine config
	if cfg.FirecrackerEngine == nil {
		return nil, fmt.Errorf("firecracker engine configuration is required")
	}

	// Validate disk_gb doesn't exceed maximum
	if cfg.Resources.DiskGB > MaxDiskGB {
		return nil, fmt.Errorf("disk_gb (%d) exceeds maximum allowed (%d GB)", cfg.Resources.DiskGB, MaxDiskGB)
	}

	// Generate ULID for sandbox
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()

	// Allocate network resources
	mac, gateway, vmIP, tapDevice := e.allocateNetwork(id)

	// Create VM directory
	vmDir := e.VMDir(id)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create VM directory: %w", err)
	}

	// Expand paths
	kernelPath := e.expandPath(cfg.FirecrackerEngine.KernelImage)
	rootfsPath := e.expandPath(cfg.FirecrackerEngine.RootFS)

	// Socket path
	socketPath := filepath.Join(vmDir, "firecracker.sock")

	e.logger.Infof("Creating Firecracker sandbox: %s", id)
	e.logger.Debugf("Network: MAC=%s, Gateway=%s, VM IP=%s, TAP=%s", mac, gateway, vmIP, tapDevice)
	e.logger.Debugf("Kernel: %s, RootFS: %s", kernelPath, rootfsPath)

	// Setup tasks if task repository is available
	if e.taskRepo != nil {
		taskNames := []string{
			"ensure_ssh_keys",
			"copy_rootfs",
			"resize_rootfs",
			"patch_rootfs_ssh",
			"patch_rootfs_dns",
			"create_tap",
			"setup_iptables",
			"spawn_firecracker",
			"configure_vm",
			"boot_vm",
			"expand_filesystem",
		}
		if err := e.taskRepo.AddTasks(ctx, id, "create", taskNames); err != nil {
			return nil, fmt.Errorf("failed to add tasks: %w", err)
		}
	}

	var createErr error
	var pid int

	// Task 1: Ensure SSH keys exist
	if err := e.executeTask(ctx, id, "create", "ensure_ssh_keys", func() error {
		e.logger.Infof("[1/11] Ensuring SSH keys exist")
		_, err := e.sshKeyManager.EnsureKeys()
		return err
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 2: Copy rootfs
	if err := e.executeTask(ctx, id, "create", "copy_rootfs", func() error {
		e.logger.Infof("[2/11] Copying rootfs to VM directory")
		return e.copyRootFS(rootfsPath, vmDir)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 3: Resize rootfs to configured disk_gb
	if err := e.executeTask(ctx, id, "create", "resize_rootfs", func() error {
		e.logger.Infof("[3/11] Resizing rootfs to %d GB", cfg.Resources.DiskGB)
		return e.resizeRootFS(vmDir, cfg.Resources.DiskGB, rootfsPath)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 4: Patch rootfs with SSH key
	if err := e.executeTask(ctx, id, "create", "patch_rootfs_ssh", func() error {
		e.logger.Infof("[4/11] Patching rootfs with SSH public key")
		return e.patchRootFSSSH(vmDir)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 5: Patch rootfs with DNS configuration
	if err := e.executeTask(ctx, id, "create", "patch_rootfs_dns", func() error {
		e.logger.Infof("[5/11] Patching rootfs with DNS configuration")
		return e.patchRootFSDNS(vmDir)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 6: Create TAP device
	if err := e.executeTask(ctx, id, "create", "create_tap", func() error {
		e.logger.Infof("[6/11] Creating TAP device: %s", tapDevice)
		return e.createTAP(tapDevice, gateway)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 7: Setup iptables
	if err := e.executeTask(ctx, id, "create", "setup_iptables", func() error {
		e.logger.Infof("[7/11] Setting up iptables NAT rules")
		return e.setupIPTables(tapDevice, gateway, vmIP)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 8: Spawn Firecracker process
	if err := e.executeTask(ctx, id, "create", "spawn_firecracker", func() error {
		e.logger.Infof("[8/11] Spawning Firecracker process")
		var err error
		pid, err = e.spawnFirecracker(vmDir, socketPath)
		return err
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 9: Configure VM via API (includes network config via kernel ip= parameter)
	if err := e.executeTask(ctx, id, "create", "configure_vm", func() error {
		e.logger.Infof("[9/11] Configuring VM via Firecracker API")
		return e.configureVM(ctx, socketPath, kernelPath, vmDir, mac, tapDevice, vmIP, gateway, cfg.Resources)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 10: Boot VM
	if err := e.executeTask(ctx, id, "create", "boot_vm", func() error {
		e.logger.Infof("[10/11] Booting VM")
		return e.bootVM(ctx, socketPath)
	}); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 11: Expand filesystem inside VM to fill resized disk
	if err := e.executeTask(ctx, id, "create", "expand_filesystem", func() error {
		e.logger.Infof("[11/11] Expanding filesystem inside VM")
		return e.expandFilesystem(ctx, vmIP)
	}); err != nil {
		createErr = err
		goto cleanup
	}

cleanup:
	if createErr != nil {
		// Cleanup on error
		e.logger.Errorf("Create failed, cleaning up: %v", createErr)
		// Try to cleanup resources - ignore errors during cleanup
		_ = e.cleanupIPTables(tapDevice, gateway, vmIP)
		_ = e.deleteTAP(tapDevice)
		// Kill firecracker process if running
		if pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Kill()
			}
		}
		_ = os.RemoveAll(vmDir)
		return nil, createErr
	}

	// Create sandbox model
	now := time.Now().UTC()
	sandbox := &model.Sandbox{
		ID:         id,
		Name:       cfg.Name,
		Status:     model.SandboxStatusRunning,
		Config:     cfg,
		CreatedAt:  now,
		StartedAt:  &now,
		PID:        pid,
		SocketPath: socketPath,
		TapDevice:  tapDevice,
		InternalIP: vmIP,
	}

	e.logger.Infof("Created Firecracker sandbox: %s (PID: %d, IP: %s)", id, pid, vmIP)

	return sandbox, nil
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
