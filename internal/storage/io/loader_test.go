package io

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
)

func TestLoader_Load(t *testing.T) {
	tests := map[string]struct {
		fs     fstest.MapFS
		path   string
		expCfg model.SandboxConfig
		expErr bool
		errMsg string
	}{
		"Valid configuration should load successfully": {
			fs: fstest.MapFS{
				"sandbox.yaml": &fstest.MapFile{
					Data: []byte(`name: test-sandbox
base: ubuntu:22.04
packages:
  - curl
  - git
env:
  FOO: bar
  DEBUG: "true"
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path: "sandbox.yaml",
			expCfg: model.SandboxConfig{
				Name:     "test-sandbox",
				Base:     "ubuntu:22.04",
				Packages: []string{"curl", "git"},
				Env:      map[string]string{"FOO": "bar", "DEBUG": "true"},
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: false,
		},
		"Minimal valid configuration should load": {
			fs: fstest.MapFS{
				"minimal.yaml": &fstest.MapFile{
					Data: []byte(`name: minimal
base: alpine:latest
resources:
  vcpus: 1
  memory_mb: 512
  disk_gb: 5
`),
				},
			},
			path: "minimal.yaml",
			expCfg: model.SandboxConfig{
				Name:     "minimal",
				Base:     "alpine:latest",
				Packages: nil,
				Env:      nil,
				Resources: model.Resources{
					VCPUs:    1,
					MemoryMB: 512,
					DiskGB:   5,
				},
			},
			expErr: false,
		},
		"Missing file should return error": {
			fs:     fstest.MapFS{},
			path:   "nonexistent.yaml",
			expErr: true,
			errMsg: "reading config file",
		},
		"Invalid YAML should return error": {
			fs: fstest.MapFS{
				"invalid.yaml": &fstest.MapFile{
					Data: []byte(`invalid: yaml: content: {}`),
				},
			},
			path:   "invalid.yaml",
			expErr: true,
			errMsg: "parsing YAML",
		},
		"Missing name should return validation error": {
			fs: fstest.MapFS{
				"no-name.yaml": &fstest.MapFile{
					Data: []byte(`base: ubuntu:22.04
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path:   "no-name.yaml",
			expErr: true,
			errMsg: "name is required",
		},
		"Missing base should return validation error": {
			fs: fstest.MapFS{
				"no-base.yaml": &fstest.MapFile{
					Data: []byte(`name: test
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path:   "no-base.yaml",
			expErr: true,
			errMsg: "base is required",
		},
		"Invalid resources (zero vcpus) should return error": {
			fs: fstest.MapFS{
				"invalid-vcpus.yaml": &fstest.MapFile{
					Data: []byte(`name: test
base: ubuntu:22.04
resources:
  vcpus: 0
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path:   "invalid-vcpus.yaml",
			expErr: true,
			errMsg: "vcpus must be positive",
		},
		"Invalid resources (negative memory) should return error": {
			fs: fstest.MapFS{
				"invalid-memory.yaml": &fstest.MapFile{
					Data: []byte(`name: test
base: ubuntu:22.04
resources:
  vcpus: 2
  memory_mb: -1024
  disk_gb: 10
`),
				},
			},
			path:   "invalid-memory.yaml",
			expErr: true,
			errMsg: "memory_mb must be positive",
		},
		"Invalid resources (zero disk) should return error": {
			fs: fstest.MapFS{
				"invalid-disk.yaml": &fstest.MapFile{
					Data: []byte(`name: test
base: ubuntu:22.04
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 0
`),
				},
			},
			path:   "invalid-disk.yaml",
			expErr: true,
			errMsg: "disk_gb must be positive",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			loader := NewLoader(tc.fs)
			cfg, err := loader.Load(context.Background(), tc.path)

			if tc.expErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expCfg, cfg)
		})
	}
}

func TestLoader_Load_ContextCancellation(t *testing.T) {
	fs := fstest.MapFS{
		"test.yaml": &fstest.MapFile{
			Data: []byte(`name: test
base: ubuntu:22.04
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
		},
	}

	loader := NewLoader(fs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := loader.Load(ctx, "test.yaml")
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
