package model_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/slok/sbx/internal/model"
)

func TestSandboxConfigValidate(t *testing.T) {
	tests := map[string]struct {
		config model.SandboxConfig
		expErr bool
	}{
		"A valid config should not fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: false,
		},

		"Missing name should fail": {
			config: model.SandboxConfig{
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: true,
		},

		"Missing base should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: true,
		},

		"Zero VCPUs should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    0,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: true,
		},

		"Negative VCPUs should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    -1,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: true,
		},

		"Zero memory should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 0,
					DiskGB:   10,
				},
			},
			expErr: true,
		},

		"Negative memory should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: -1,
					DiskGB:   10,
				},
			},
			expErr: true,
		},

		"Zero disk should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   0,
				},
			},
			expErr: true,
		},

		"Negative disk should fail": {
			config: model.SandboxConfig{
				Name: "test",
				Base: "ubuntu-22.04",
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   -1,
				},
			},
			expErr: true,
		},

		"Config with optional fields should not fail": {
			config: model.SandboxConfig{
				Name:     "test",
				Base:     "ubuntu-22.04",
				Packages: []string{"git", "curl"},
				Env:      map[string]string{"EDITOR": "vim"},
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			err := test.config.Validate()

			if test.expErr {
				assert.Error(err)
				assert.True(errors.Is(err, model.ErrNotValid))
			} else {
				assert.NoError(err)
			}
		})
	}
}
