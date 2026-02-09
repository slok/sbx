package model

import (
	"fmt"
	"time"
)

// SandboxStatus represents the status of a sandbox.
type SandboxStatus string

const (
	// SandboxStatusPending indicates the sandbox is being created.
	SandboxStatusPending SandboxStatus = "pending"
	// SandboxStatusRunning indicates the sandbox is running.
	SandboxStatusRunning SandboxStatus = "running"
	// SandboxStatusStopped indicates the sandbox is stopped (including freshly created).
	SandboxStatusStopped SandboxStatus = "stopped"
	// SandboxStatusFailed indicates the sandbox failed.
	SandboxStatusFailed SandboxStatus = "failed"
)

// Sandbox represents a sandbox instance.
type Sandbox struct {
	ID        string
	Name      string
	Status    SandboxStatus
	Config    SandboxConfig
	CreatedAt time.Time
	StartedAt *time.Time
	StoppedAt *time.Time

	// Firecracker-specific fields
	PID        int    // Firecracker process ID
	SocketPath string // API socket path (e.g., ~/.sbx/vms/<id>/firecracker.sock)
	TapDevice  string // TAP device name (e.g., sbx-a3f2)
	InternalIP string // VM's IP address (e.g., 10.163.242.2)
}

// SandboxConfig is the static configuration for creating a sandbox.
// These settings are immutable after creation.
type SandboxConfig struct {
	Name              string
	FirecrackerEngine *FirecrackerEngineConfig
	Resources         Resources
}

// SessionConfig is the dynamic configuration applied when starting a sandbox.
// These settings can change between starts and will be extended with
// env vars, file copies, etc. in the future.
type SessionConfig struct {
	Name string
	Env  map[string]string
}

// FirecrackerEngineConfig contains Firecracker-specific engine configuration.
type FirecrackerEngineConfig struct {
	RootFS      string
	KernelImage string
}

// Resources defines the compute resources for a sandbox.
type Resources struct {
	VCPUs    float64
	MemoryMB int
	DiskGB   int
}

// Validate validates the sandbox configuration.
func (c *SandboxConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required: %w", ErrNotValid)
	}

	if c.FirecrackerEngine == nil {
		return fmt.Errorf("firecracker engine configuration is required: %w", ErrNotValid)
	}

	// Validate engine-specific configuration
	if c.FirecrackerEngine.RootFS == "" {
		return fmt.Errorf("firecracker engine root_fs is required: %w", ErrNotValid)
	}
	if c.FirecrackerEngine.KernelImage == "" {
		return fmt.Errorf("firecracker engine kernel_image is required: %w", ErrNotValid)
	}

	// Validate resources
	if c.Resources.VCPUs <= 0 {
		return fmt.Errorf("vcpus must be positive: %w", ErrNotValid)
	}
	if c.Resources.MemoryMB <= 0 {
		return fmt.Errorf("memory_mb must be positive: %w", ErrNotValid)
	}
	if c.Resources.DiskGB <= 0 {
		return fmt.Errorf("disk_gb must be positive: %w", ErrNotValid)
	}
	return nil
}
