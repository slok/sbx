package io

import (
	"context"
	"fmt"
	"io/fs"

	"gopkg.in/yaml.v3"

	"github.com/slok/sbx/internal/model"
)

// ConfigYAMLRepository loads sandbox configuration from YAML files.
type ConfigYAMLRepository struct {
	fs fs.FS
}

// NewConfigYAMLRepository creates a new YAML config repository.
func NewConfigYAMLRepository(filesystem fs.FS) *ConfigYAMLRepository {
	return &ConfigYAMLRepository{fs: filesystem}
}

// GetConfig loads a sandbox configuration from a YAML file and returns a validated domain model.
func (r *ConfigYAMLRepository) GetConfig(ctx context.Context, path string) (model.SandboxConfig, error) {
	data, err := fs.ReadFile(r.fs, path)
	if err != nil {
		return model.SandboxConfig{}, fmt.Errorf("reading config file: %w", err)
	}

	if ctx.Err() != nil {
		return model.SandboxConfig{}, ctx.Err()
	}

	var cfg SandboxConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return model.SandboxConfig{}, fmt.Errorf("parsing YAML: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return model.SandboxConfig{}, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg.toModel(), nil
}

// SandboxConfig represents the YAML structure for sandbox configuration.
type SandboxConfig struct {
	Name      string            `yaml:"name"`
	Engine    EngineConfig      `yaml:"engine"`
	Packages  []string          `yaml:"packages"`
	Env       map[string]string `yaml:"env"`
	Resources ResourcesConfig   `yaml:"resources"`
}

// EngineConfig represents the YAML structure for engine configuration.
type EngineConfig struct {
	Docker      *DockerEngineConfig      `yaml:"docker,omitempty"`
	Firecracker *FirecrackerEngineConfig `yaml:"firecracker,omitempty"`
}

// DockerEngineConfig represents the YAML structure for Docker engine configuration.
type DockerEngineConfig struct {
	Image string `yaml:"image"`
}

// FirecrackerEngineConfig represents the YAML structure for Firecracker engine configuration.
type FirecrackerEngineConfig struct {
	RootFS      string `yaml:"root_fs"`
	KernelImage string `yaml:"kernel_image"`
}

func (c SandboxConfig) validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Ensure exactly one engine is specified
	engineCount := 0
	if c.Engine.Docker != nil {
		engineCount++
	}
	if c.Engine.Firecracker != nil {
		engineCount++
	}
	if engineCount == 0 {
		return fmt.Errorf("exactly one engine must be specified (docker or firecracker)")
	}
	if engineCount > 1 {
		return fmt.Errorf("only one engine can be specified at a time")
	}

	// Validate engine-specific configuration
	if c.Engine.Docker != nil {
		if c.Engine.Docker.Image == "" {
			return fmt.Errorf("docker engine image is required")
		}
	}
	if c.Engine.Firecracker != nil {
		if c.Engine.Firecracker.RootFS == "" {
			return fmt.Errorf("firecracker engine root_fs is required")
		}
		if c.Engine.Firecracker.KernelImage == "" {
			return fmt.Errorf("firecracker engine kernel_image is required")
		}
	}

	if err := c.Resources.validate(); err != nil {
		return fmt.Errorf("resources: %w", err)
	}
	return nil
}

func (c SandboxConfig) toModel() model.SandboxConfig {
	cfg := model.SandboxConfig{
		Name:     c.Name,
		Packages: c.Packages,
		Env:      c.Env,
		Resources: model.Resources{
			VCPUs:    c.Resources.VCPUs,
			MemoryMB: c.Resources.MemoryMB,
			DiskGB:   c.Resources.DiskGB,
		},
	}

	// Convert engine configuration
	if c.Engine.Docker != nil {
		cfg.DockerEngine = &model.DockerEngineConfig{
			Image: c.Engine.Docker.Image,
		}
	}
	if c.Engine.Firecracker != nil {
		cfg.FirecrackerEngine = &model.FirecrackerEngineConfig{
			RootFS:      c.Engine.Firecracker.RootFS,
			KernelImage: c.Engine.Firecracker.KernelImage,
		}
	}

	return cfg
}

// ResourcesConfig represents the YAML structure for resource configuration.
type ResourcesConfig struct {
	VCPUs    int `yaml:"vcpus"`
	MemoryMB int `yaml:"memory_mb"`
	DiskGB   int `yaml:"disk_gb"`
}

func (r ResourcesConfig) validate() error {
	if r.VCPUs <= 0 {
		return fmt.Errorf("vcpus must be positive, got: %d", r.VCPUs)
	}
	if r.MemoryMB <= 0 {
		return fmt.Errorf("memory_mb must be positive, got: %d", r.MemoryMB)
	}
	if r.DiskGB <= 0 {
		return fmt.Errorf("disk_gb must be positive, got: %d", r.DiskGB)
	}
	return nil
}
