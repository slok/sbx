package io

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
)

func TestConfigYAMLRepository_GetConfig(t *testing.T) {
	tests := map[string]struct {
		fs     fstest.MapFS
		path   string
		expCfg model.SandboxConfig
		expErr bool
		errMsg string
	}{
		"Valid Docker configuration should load successfully": {
			fs: fstest.MapFS{
				"sandbox.yaml": &fstest.MapFile{
					Data: []byte(`name: test-sandbox
engine:
  docker:
    image: ubuntu:22.04
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
				Name: "test-sandbox",
				DockerEngine: &model.DockerEngineConfig{
					Image: "ubuntu:22.04",
				},
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
		"Valid Firecracker configuration should load successfully": {
			fs: fstest.MapFS{
				"firecracker.yaml": &fstest.MapFile{
					Data: []byte(`name: fc-sandbox
engine:
  firecracker:
    root_fs: /path/to/rootfs.ext4
    kernel_image: /path/to/vmlinux
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path: "firecracker.yaml",
			expCfg: model.SandboxConfig{
				Name: "fc-sandbox",
				FirecrackerEngine: &model.FirecrackerEngineConfig{
					RootFS:      "/path/to/rootfs.ext4",
					KernelImage: "/path/to/vmlinux",
				},
				Resources: model.Resources{
					VCPUs:    2,
					MemoryMB: 2048,
					DiskGB:   10,
				},
			},
			expErr: false,
		},
		"Minimal Docker configuration should load": {
			fs: fstest.MapFS{
				"minimal.yaml": &fstest.MapFile{
					Data: []byte(`name: minimal
engine:
  docker:
    image: alpine:latest
resources:
  vcpus: 1
  memory_mb: 512
  disk_gb: 5
`),
				},
			},
			path: "minimal.yaml",
			expCfg: model.SandboxConfig{
				Name: "minimal",
				DockerEngine: &model.DockerEngineConfig{
					Image: "alpine:latest",
				},
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
					Data: []byte(`engine:
  docker:
    image: ubuntu:22.04
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
		"Missing engine should return validation error": {
			fs: fstest.MapFS{
				"no-engine.yaml": &fstest.MapFile{
					Data: []byte(`name: test
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path:   "no-engine.yaml",
			expErr: true,
			errMsg: "exactly one engine must be specified",
		},
		"Both engines specified should return validation error": {
			fs: fstest.MapFS{
				"both-engines.yaml": &fstest.MapFile{
					Data: []byte(`name: test
engine:
  docker:
    image: ubuntu:22.04
  firecracker:
    root_fs: /path/to/rootfs.ext4
    kernel_image: /path/to/vmlinux
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path:   "both-engines.yaml",
			expErr: true,
			errMsg: "only one engine can be specified",
		},
		"Docker engine with missing image should return error": {
			fs: fstest.MapFS{
				"no-image.yaml": &fstest.MapFile{
					Data: []byte(`name: test
engine:
  docker:
    image: ""
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
				},
			},
			path:   "no-image.yaml",
			expErr: true,
			errMsg: "docker engine image is required",
		},
		"Invalid resources (zero vcpus) should return error": {
			fs: fstest.MapFS{
				"invalid-vcpus.yaml": &fstest.MapFile{
					Data: []byte(`name: test
engine:
  docker:
    image: ubuntu:22.04
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
engine:
  docker:
    image: ubuntu:22.04
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
engine:
  docker:
    image: ubuntu:22.04
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
			repo := NewConfigYAMLRepository(tc.fs)
			cfg, err := repo.GetConfig(context.Background(), tc.path)

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

func TestConfigYAMLRepository_GetConfig_ContextCancellation(t *testing.T) {
	fs := fstest.MapFS{
		"test.yaml": &fstest.MapFile{
			Data: []byte(`name: test
engine:
  docker:
    image: ubuntu:22.04
resources:
  vcpus: 2
  memory_mb: 2048
  disk_gb: 10
`),
		},
	}

	repo := NewConfigYAMLRepository(fs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := repo.GetConfig(ctx, "test.yaml")
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
