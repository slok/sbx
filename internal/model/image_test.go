package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/slok/sbx/internal/model"
)

func TestImageManifestArtifactsAccess(t *testing.T) {
	manifest := model.ImageManifest{
		SchemaVersion: 1,
		Version:       "v0.1.0",
		Artifacts: map[string]model.ArchArtifacts{
			"x86_64": {
				Kernel: model.KernelInfo{
					File:      "vmlinux-x86_64",
					Version:   "6.1.155",
					Source:    "firecracker-ci/v1.15",
					SizeBytes: 44279576,
				},
				Rootfs: model.RootfsInfo{
					File:          "rootfs-x86_64.ext4",
					Distro:        "alpine",
					DistroVersion: "3.23",
					Profile:       "balanced",
					SizeBytes:     679034880,
				},
			},
		},
		Firecracker: model.FirecrackerInfo{
			Version: "v1.14.1",
			Source:  "github.com/firecracker-microvm/firecracker",
		},
		Build: model.BuildInfo{
			Date:   "2026-02-08T09:54:17Z",
			Commit: "adc9bc1",
		},
	}

	arch, ok := manifest.Artifacts["x86_64"]
	assert.True(t, ok)
	assert.Equal(t, "vmlinux-x86_64", arch.Kernel.File)
	assert.Equal(t, "rootfs-x86_64.ext4", arch.Rootfs.File)
	assert.Equal(t, "v1.14.1", manifest.Firecracker.Version)
	assert.Equal(t, "adc9bc1", manifest.Build.Commit)
}
