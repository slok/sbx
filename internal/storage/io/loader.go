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
	Base      string            `yaml:"base"`
	Packages  []string          `yaml:"packages"`
	Env       map[string]string `yaml:"env"`
	Resources ResourcesConfig   `yaml:"resources"`
}

func (c SandboxConfig) validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.Base == "" {
		return fmt.Errorf("base is required")
	}
	if err := c.Resources.validate(); err != nil {
		return fmt.Errorf("resources: %w", err)
	}
	return nil
}

func (c SandboxConfig) toModel() model.SandboxConfig {
	return model.SandboxConfig{
		Name:     c.Name,
		Base:     c.Base,
		Packages: c.Packages,
		Env:      c.Env,
		Resources: model.Resources{
			VCPUs:    c.Resources.VCPUs,
			MemoryMB: c.Resources.MemoryMB,
			DiskGB:   c.Resources.DiskGB,
		},
	}
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
