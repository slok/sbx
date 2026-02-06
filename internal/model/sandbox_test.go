package model_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/slok/sbx/internal/model"
)

func TestSandboxConfigValidate(t *testing.T) {
	base := model.SandboxConfig{
		Name: "test",
		FirecrackerEngine: &model.FirecrackerEngineConfig{
			RootFS:      "/images/rootfs.ext4",
			KernelImage: "/images/vmlinux",
		},
		Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 10},
	}

	tests := map[string]struct {
		cfg    model.SandboxConfig
		expErr bool
	}{
		"valid config": {cfg: base},
		"missing name": {
			cfg:    model.SandboxConfig{FirecrackerEngine: base.FirecrackerEngine, Resources: base.Resources},
			expErr: true,
		},
		"missing engine": {
			cfg:    model.SandboxConfig{Name: "test", Resources: base.Resources},
			expErr: true,
		},
		"missing rootfs": {
			cfg: model.SandboxConfig{
				Name:              "test",
				FirecrackerEngine: &model.FirecrackerEngineConfig{KernelImage: "/images/vmlinux"},
				Resources:         base.Resources,
			},
			expErr: true,
		},
		"invalid resources": {
			cfg: model.SandboxConfig{
				Name:              "test",
				FirecrackerEngine: base.FirecrackerEngine,
				Resources:         model.Resources{VCPUs: 0, MemoryMB: 0, DiskGB: 0},
			},
			expErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, model.ErrNotValid))
				return
			}
			assert.NoError(t, err)
		})
	}
}
