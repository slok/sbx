package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/slok/sbx/internal/conventions"
	"github.com/slok/sbx/internal/model"
)

// Firecracker API types
// See: https://github.com/firecracker-microvm/firecracker/blob/main/src/api_server/swagger/firecracker.yaml

// BootSource is the boot source configuration.
type BootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args,omitempty"`
}

// Drive is a block device configuration.
type Drive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

// MachineConfig is the machine configuration.
type MachineConfig struct {
	VCPUCount  int  `json:"vcpu_count"`
	MemSizeMib int  `json:"mem_size_mib"`
	Smt        bool `json:"smt,omitempty"`
}

// NetworkInterface is a network interface configuration.
type NetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	GuestMAC    string `json:"guest_mac"`
	HostDevName string `json:"host_dev_name"`
}

// InstanceActionInfo is an action request.
type InstanceActionInfo struct {
	ActionType string `json:"action_type"`
}

// findFirecrackerBinary finds the firecracker binary.
func (e *Engine) findFirecrackerBinary() (string, error) {
	// 1. Check explicit config
	if e.firecrackerBinary != "" {
		if _, err := os.Stat(e.firecrackerBinary); err == nil {
			return e.firecrackerBinary, nil
		}
	}

	// 2. Check ./bin directory
	if cwd, err := os.Getwd(); err == nil {
		binPath := filepath.Join(cwd, "bin", "firecracker")
		if _, err := os.Stat(binPath); err == nil {
			return binPath, nil
		}
	}

	// 3. Check PATH
	if path, err := exec.LookPath("firecracker"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("firecracker binary not found")
}

// spawnFirecracker spawns the Firecracker process.
func (e *Engine) spawnFirecracker(vmDir, socketPath string) (int, error) {
	fcBinary, err := e.findFirecrackerBinary()
	if err != nil {
		return 0, err
	}

	// Remove existing socket if present
	_ = os.Remove(socketPath)

	// Create log file
	logPath := filepath.Join(vmDir, conventions.LogFile)
	logFile, err := os.Create(logPath)
	if err != nil {
		return 0, fmt.Errorf("could not create log file: %w", err)
	}

	// Spawn firecracker process
	cmd := exec.Command(fcBinary, "--api-sock", socketPath)
	cmd.Dir = vmDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("failed to start firecracker: %w", err)
	}

	pid := cmd.Process.Pid

	// Write PID file
	pidPath := filepath.Join(vmDir, conventions.PIDFile)
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		e.logger.Warningf("Could not write PID file: %v", err)
	}

	// Wait for socket to be available
	if err := e.waitForSocket(socketPath, 10*time.Second); err != nil {
		// Kill process if socket never appeared
		_ = cmd.Process.Kill()
		return 0, fmt.Errorf("socket not available: %w", err)
	}

	e.logger.Debugf("Spawned Firecracker process: PID=%d, socket=%s", pid, socketPath)
	return pid, nil
}

// waitForSocket waits for the Unix socket to become available.
func (e *Engine) waitForSocket(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for socket %s", socketPath)
}

// configureVM configures the VM via the Firecracker API.
// vmIP and gateway are used to configure networking via kernel boot parameters,
// which works for any distro (Ubuntu, Alpine, etc.) without post-boot SSH config.
func (e *Engine) configureVM(ctx context.Context, socketPath, kernelPath, vmDir, mac, tapDevice, vmIP, gateway string, resources model.Resources) error {
	client := e.newUnixHTTPClient(socketPath)

	// 1. Configure boot source with network config via kernel ip= parameter
	// Format: ip=<client-ip>:<server-ip>:<gateway>:<netmask>:<hostname>:<device>:<autoconf>
	// This configures networking before init runs, works for any distro
	// Note: init uses /usr/sbin/sbx-init since /sbin is typically a symlink to usr/sbin
	bootArgs := fmt.Sprintf("console=ttyS0 reboot=k panic=1 pci=off init=/usr/sbin/sbx-init ip=%s::%s:255.255.255.0::eth0:off", vmIP, gateway)
	bootSource := BootSource{
		KernelImagePath: kernelPath,
		BootArgs:        bootArgs,
	}
	if err := e.apiPUT(ctx, client, "/boot-source", bootSource); err != nil {
		return fmt.Errorf("failed to configure boot source: %w", err)
	}

	// 2. Configure rootfs drive
	rootfsPath := e.RootFSPath(vmDir)
	drive := Drive{
		DriveID:      "rootfs",
		PathOnHost:   rootfsPath,
		IsRootDevice: true,
		IsReadOnly:   false,
	}
	if err := e.apiPUT(ctx, client, "/drives/rootfs", drive); err != nil {
		return fmt.Errorf("failed to configure rootfs drive: %w", err)
	}

	// 3. Configure machine
	// Note: Firecracker only supports whole VCPUs, so we round to nearest integer
	vcpuCount := int(resources.VCPUs + 0.5) // Round to nearest
	if vcpuCount < 1 {
		vcpuCount = 1 // Minimum 1 vCPU
	}
	machineConfig := MachineConfig{
		VCPUCount:  vcpuCount,
		MemSizeMib: resources.MemoryMB,
	}
	if err := e.apiPUT(ctx, client, "/machine-config", machineConfig); err != nil {
		return fmt.Errorf("failed to configure machine: %w", err)
	}

	// 4. Configure network interface
	netIface := NetworkInterface{
		IfaceID:     "eth0",
		GuestMAC:    mac,
		HostDevName: tapDevice,
	}
	if err := e.apiPUT(ctx, client, "/network-interfaces/eth0", netIface); err != nil {
		return fmt.Errorf("failed to configure network interface: %w", err)
	}

	e.logger.Debugf("Configured VM via Firecracker API")
	return nil
}

// bootVM boots the VM by sending the start action.
func (e *Engine) bootVM(ctx context.Context, socketPath string) error {
	client := e.newUnixHTTPClient(socketPath)

	action := InstanceActionInfo{
		ActionType: "InstanceStart",
	}
	if err := e.apiPUT(ctx, client, "/actions", action); err != nil {
		return fmt.Errorf("failed to boot VM: %w", err)
	}

	e.logger.Debugf("VM boot initiated")
	return nil
}

// newUnixHTTPClient creates an HTTP client that connects via Unix socket.
func (e *Engine) newUnixHTTPClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 30 * time.Second,
	}
}

// apiPUT sends a PUT request to the Firecracker API.
func (e *Engine) apiPUT(ctx context.Context, client *http.Client, path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	// Note: We use http://localhost as a placeholder; the actual connection
	// is via Unix socket, so the host doesn't matter.
	url := "http://localhost" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, buf.String())
	}

	return nil
}
