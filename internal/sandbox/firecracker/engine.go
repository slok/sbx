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
	// SnapshotsDir is the subdirectory for snapshot files.
	SnapshotsDir = "snapshots"
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

// SnapshotsPath returns the path to the snapshots directory.
func (e *Engine) SnapshotsPath() string {
	return filepath.Join(e.dataDir, SnapshotsDir)
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

	var createErr error

	// Task 1: Ensure SSH keys exist
	e.logger.Infof("[1/4] Ensuring SSH keys exist")
	if _, err := e.sshKeyManager.EnsureKeys(); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 2: Copy rootfs
	e.logger.Infof("[2/4] Copying rootfs to VM directory")
	if err := e.copyRootFS(rootfsPath, vmDir); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 3: Resize rootfs to configured disk_gb
	e.logger.Infof("[3/4] Resizing rootfs to %d GB", cfg.Resources.DiskGB)
	if err := e.resizeRootFS(vmDir, cfg.Resources.DiskGB, rootfsPath); err != nil {
		createErr = err
		goto cleanup
	}

	// Task 4: Patch rootfs with SSH key
	e.logger.Infof("[4/4] Patching rootfs with SSH public key")
	if err := e.patchRootFSSSH(vmDir); err != nil {
		createErr = err
		goto cleanup
	}

cleanup:
	if createErr != nil {
		// Cleanup on error
		e.logger.Errorf("Create failed, cleaning up: %v", createErr)
		_ = os.RemoveAll(vmDir)
		return nil, createErr
	}

	// Create sandbox model in "created" status (not running yet).
	// Start will handle TAP, iptables, spawning, and booting the VM.
	now := time.Now().UTC()
	sandbox := &model.Sandbox{
		ID:         id,
		Name:       cfg.Name,
		Status:     model.SandboxStatusCreated,
		Config:     cfg,
		CreatedAt:  now,
		SocketPath: socketPath,
		TapDevice:  tapDevice,
		InternalIP: vmIP,
	}

	e.logger.Infof("Created Firecracker sandbox: %s (IP: %s)", id, vmIP)

	return sandbox, nil
}
