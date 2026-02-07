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

func TestTablePrinterPrintMessage(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintMessage("ok")
	require.NoError(t, err)
	assert.Equal(t, "ok", strings.TrimSpace(buf.String()))
}
