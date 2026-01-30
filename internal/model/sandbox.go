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
	// SandboxStatusStopped indicates the sandbox is stopped.
	SandboxStatusStopped SandboxStatus = "stopped"
	// SandboxStatusFailed indicates the sandbox failed.
	SandboxStatusFailed SandboxStatus = "failed"
)

// Sandbox represents a microVM sandbox instance.
type Sandbox struct {
	ID        string
	Name      string
	Status    SandboxStatus
	Config    SandboxConfig
	CreatedAt time.Time
	StartedAt *time.Time
	StoppedAt *time.Time
	Error     string
}

// SandboxConfig is the configuration for creating a sandbox.
type SandboxConfig struct {
	Name      string            `yaml:"name"`
	Base      string            `yaml:"base"`
	Packages  []string          `yaml:"packages"`
	Env       map[string]string `yaml:"env"`
	Resources Resources         `yaml:"resources"`
}

// Resources defines the compute resources for a sandbox.
type Resources struct {
	VCPUs    int `yaml:"vcpus"`
	MemoryMB int `yaml:"memory_mb"`
	DiskGB   int `yaml:"disk_gb"`
}

// Validate validates the sandbox configuration.
func (c *SandboxConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required: %w", ErrNotValid)
	}
	if c.Base == "" {
		return fmt.Errorf("base is required: %w", ErrNotValid)
	}
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
