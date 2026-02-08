package printer_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/printer"
)

func sandboxFixture() model.Sandbox {
	createdAt := time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC)
	return model.Sandbox{
		ID:        "01234567890ABCDEFGHIJKLMNOP",
		Name:      "my-sandbox",
		Status:    model.SandboxStatusRunning,
		CreatedAt: createdAt,
		Config: model.SandboxConfig{
			Name: "my-sandbox",
			FirecrackerEngine: &model.FirecrackerEngineConfig{
				RootFS:      "/images/rootfs.ext4",
				KernelImage: "/images/vmlinux",
			},
			Resources: model.Resources{VCPUs: 2, MemoryMB: 2048, DiskGB: 10},
		},
	}
}

func TestTablePrinterPrintStatus(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintStatus(sandboxFixture())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Engine:     firecracker")
	assert.Contains(t, out, "RootFS:     /images/rootfs.ext4")
	assert.Contains(t, out, "Kernel:     /images/vmlinux")
}

func TestJSONPrinterPrintStatus(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewJSONPrinter(&buf)

	err := p.PrintStatus(sandboxFixture())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"type": "firecracker"`)
	assert.Contains(t, out, `"root_fs": "/images/rootfs.ext4"`)
	assert.Contains(t, out, `"kernel_image": "/images/vmlinux"`)
}

func snapshotFixtures() []model.Snapshot {
	return []model.Snapshot{
		{
			ID:                 "snap-001",
			Name:               "my-snap",
			Path:               "/home/user/.sbx/snapshots/snap-001.ext4",
			SourceSandboxID:    "sb-001",
			SourceSandboxName:  "my-sandbox",
			VirtualSizeBytes:   10 * 1024 * 1024 * 1024, // 10 GB
			AllocatedSizeBytes: 700 * 1024 * 1024,       // 700 MB
			CreatedAt:          time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:                 "snap-002",
			Name:               "other-snap",
			Path:               "/home/user/.sbx/snapshots/snap-002.ext4",
			SourceSandboxID:    "sb-002",
			SourceSandboxName:  "other-sandbox",
			VirtualSizeBytes:   5 * 1024 * 1024 * 1024, // 5 GB
			AllocatedSizeBytes: 300 * 1024 * 1024,      // 300 MB
			CreatedAt:          time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
		},
	}
}

func TestTablePrinterPrintSnapshotList(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintSnapshotList(snapshotFixtures())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "SOURCE")
	assert.Contains(t, out, "VIRT SIZE")
	assert.Contains(t, out, "DISK SIZE")
	assert.Contains(t, out, "CREATED")
	assert.Contains(t, out, "my-snap")
	assert.Contains(t, out, "my-sandbox")
	assert.Contains(t, out, "10.0 GB")
	assert.Contains(t, out, "700.0 MB")
	assert.Contains(t, out, "other-snap")
	assert.Contains(t, out, "other-sandbox")
}

func TestTablePrinterPrintSnapshotListEmpty(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintSnapshotList([]model.Snapshot{})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestJSONPrinterPrintSnapshotList(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewJSONPrinter(&buf)

	err := p.PrintSnapshotList(snapshotFixtures())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"id": "snap-001"`)
	assert.Contains(t, out, `"name": "my-snap"`)
	assert.Contains(t, out, `"source_sandbox_id": "sb-001"`)
	assert.Contains(t, out, `"source_sandbox_name": "my-sandbox"`)
	assert.Contains(t, out, `"virtual_size_bytes": 10737418240`)
	assert.Contains(t, out, `"allocated_size_bytes": 734003200`)
	assert.Contains(t, out, `"name": "other-snap"`)
}

func imageReleaseFixtures() []model.ImageRelease {
	return []model.ImageRelease{
		{Version: "v0.1.0", Installed: true},
		{Version: "v0.2.0-rc.1", Installed: false},
		{Version: "v0.0.1", Installed: true},
	}
}

func imageManifestFixture() model.ImageManifest {
	return model.ImageManifest{
		Version: "v0.1.0",
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
}

func TestTablePrinterPrintImageList(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintImageList(imageReleaseFixtures())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "VERSION")
	assert.Contains(t, out, "INSTALLED")
	assert.Contains(t, out, "v0.1.0")
	assert.Contains(t, out, "v0.2.0-rc.1")
	assert.Contains(t, out, "yes")
	assert.Contains(t, out, "no")
}

func TestTablePrinterPrintImageListEmpty(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintImageList([]model.ImageRelease{})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestTablePrinterPrintImageInspect(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintImageInspect(imageManifestFixture())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Version:      v0.1.0")
	assert.Contains(t, out, "Firecracker:  v1.14.1")
	assert.Contains(t, out, "x86_64")
	assert.Contains(t, out, "vmlinux-x86_64")
	assert.Contains(t, out, "6.1.155")
	assert.Contains(t, out, "rootfs-x86_64.ext4")
	assert.Contains(t, out, "alpine")
	assert.Contains(t, out, "balanced")
	assert.Contains(t, out, "adc9bc1")
}

func TestJSONPrinterPrintImageList(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewJSONPrinter(&buf)

	err := p.PrintImageList(imageReleaseFixtures())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"version": "v0.1.0"`)
	assert.Contains(t, out, `"installed": true`)
	assert.Contains(t, out, `"version": "v0.2.0-rc.1"`)
	assert.Contains(t, out, `"installed": false`)
}

func TestJSONPrinterPrintImageInspect(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewJSONPrinter(&buf)

	err := p.PrintImageInspect(imageManifestFixture())
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"version": "v0.1.0"`)
	assert.Contains(t, out, `"version": "6.1.155"`)
	assert.Contains(t, out, `"file": "vmlinux-x86_64"`)
	assert.Contains(t, out, `"distro": "alpine"`)
	assert.Contains(t, out, `"distro_version": "3.23"`)
	assert.Contains(t, out, `"profile": "balanced"`)
	assert.Contains(t, out, `"version": "v1.14.1"`)
	assert.Contains(t, out, `"commit": "adc9bc1"`)
}

func TestTablePrinterPrintMessage(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintMessage("ok")
	require.NoError(t, err)
	assert.Equal(t, "ok", strings.TrimSpace(buf.String()))
}
