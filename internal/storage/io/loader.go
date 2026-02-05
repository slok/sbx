package io

import (
	"context"
	"fmt"
	"io/fs"

	"gopkg.in/yaml.v3"

	"github.com/slok/sbx/internal/model"
)

// SessionYAMLRepository loads session configuration from YAML files.
type SessionYAMLRepository struct {
	fs fs.FS
}

// NewSessionYAMLRepository creates a new YAML session config repository.
func NewSessionYAMLRepository(filesystem fs.FS) *SessionYAMLRepository {
	return &SessionYAMLRepository{fs: filesystem}
}

// GetSessionConfig loads a session configuration from a YAML file and returns a validated domain model.
func (r *SessionYAMLRepository) GetSessionConfig(ctx context.Context, path string) (model.SessionConfig, error) {
	data, err := fs.ReadFile(r.fs, path)
	if err != nil {
		return model.SessionConfig{}, fmt.Errorf("reading session config file: %w", err)
	}

	if ctx.Err() != nil {
		return model.SessionConfig{}, ctx.Err()
	}

	var cfg SessionConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return model.SessionConfig{}, fmt.Errorf("parsing YAML: %w", err)
	}

	return cfg.toModel(), nil
}

// SessionConfig represents the YAML structure for session configuration.
type SessionConfig struct {
	Name string            `yaml:"name"`
	Env  map[string]string `yaml:"env"`
}

func (c SessionConfig) toModel() model.SessionConfig {
	return model.SessionConfig{
		Name: c.Name,
		Env:  c.Env,
	}
}
