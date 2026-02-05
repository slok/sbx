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

func TestTablePrinterPrintMessage(t *testing.T) {
	var buf bytes.Buffer
	p := printer.NewTablePrinter(&buf)

	err := p.PrintMessage("ok")
	require.NoError(t, err)
	assert.Equal(t, "ok", strings.TrimSpace(buf.String()))
}
