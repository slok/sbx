package printer_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/printer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTablePrinter_PrintList(t *testing.T) {
	createdAt := time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 1, 30, 10, 0, 5, 0, time.UTC)

	tests := map[string]struct {
		sandboxes    []model.Sandbox
		expectedCols []string
	}{
		"empty list prints nothing": {
			sandboxes:    []model.Sandbox{},
			expectedCols: nil,
		},
		"single sandbox": {
			sandboxes: []model.Sandbox{
				{
					ID:        "01234567890ABCDEFGHIJKLMNOP",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
				},
			},
			expectedCols: []string{"NAME", "STATUS", "CREATED", "my-sandbox", "running"},
		},
		"multiple sandboxes": {
			sandboxes: []model.Sandbox{
				{
					ID:        "01234567890ABCDEFGHIJKLMNOP",
					Name:      "sandbox-1",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				},
				{
					ID:        "11234567890ABCDEFGHIJKLMNOP",
					Name:      "sandbox-2",
					Status:    model.SandboxStatusStopped,
					CreatedAt: createdAt.Add(-2 * time.Hour),
				},
			},
			expectedCols: []string{"NAME", "STATUS", "CREATED", "sandbox-1", "running", "sandbox-2", "stopped"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			var buf bytes.Buffer
			p := printer.NewTablePrinter(&buf)

			err := p.PrintList(test.sandboxes)
			assert.NoError(err)

			output := buf.String()
			for _, col := range test.expectedCols {
				assert.Contains(output, col)
			}
		})
	}
}

func TestTablePrinter_PrintStatus(t *testing.T) {
	createdAt := time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 1, 30, 10, 0, 5, 0, time.UTC)
	stoppedAt := time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		sandbox        model.Sandbox
		expectedFields []string
		notExpected    []string
	}{
		"running sandbox with all fields": {
			sandbox: model.Sandbox{
				ID:        "01234567890ABCDEFGHIJKLMNOP",
				Name:      "my-sandbox",
				Status:    model.SandboxStatusRunning,
				CreatedAt: createdAt,
				StartedAt: &startedAt,
				Config: model.SandboxConfig{
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10,
					},
				},
			},
			expectedFields: []string{
				"Name:", "my-sandbox",
				"ID:", "01234567890ABCDEFGHIJKLMNOP",
				"Status:", "running",
				"Base:", "ubuntu:22.04",
				"VCPUs:", "2",
				"Memory:", "2048 MB",
				"Disk:", "10 GB",
				"Created:", "2026-01-30 10:00:00 UTC",
				"Started:", "2026-01-30 10:00:05 UTC",
			},
			notExpected: []string{"Stopped:", "Error:"},
		},
		"stopped sandbox": {
			sandbox: model.Sandbox{
				ID:        "01234567890ABCDEFGHIJKLMNOP",
				Name:      "stopped-sandbox",
				Status:    model.SandboxStatusStopped,
				CreatedAt: createdAt,
				StartedAt: &startedAt,
				StoppedAt: &stoppedAt,
				Config: model.SandboxConfig{
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10,
					},
				},
			},
			expectedFields: []string{
				"Name:", "stopped-sandbox",
				"Status:", "stopped",
				"Started:", "2026-01-30 10:00:05 UTC",
				"Stopped:", "2026-01-30 12:00:00 UTC",
			},
			notExpected: []string{"Error:"},
		},
		"failed sandbox with error": {
			sandbox: model.Sandbox{
				ID:        "01234567890ABCDEFGHIJKLMNOP",
				Name:      "failed-sandbox",
				Status:    model.SandboxStatusFailed,
				CreatedAt: createdAt,
				Error:     "failed to provision VM",
				Config: model.SandboxConfig{
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10,
					},
				},
			},
			expectedFields: []string{
				"Status:", "failed",
				"Error:", "failed to provision VM",
			},
			notExpected: []string{"Started:", "Stopped:"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			var buf bytes.Buffer
			p := printer.NewTablePrinter(&buf)

			err := p.PrintStatus(test.sandbox)
			assert.NoError(err)

			output := buf.String()
			for _, field := range test.expectedFields {
				assert.Contains(output, field)
			}
			for _, field := range test.notExpected {
				assert.NotContains(output, field)
			}
		})
	}
}

func TestTablePrinter_PrintMessage(t *testing.T) {
	tests := map[string]struct {
		message  string
		expected string
	}{
		"simple message": {
			message:  "Stopped sandbox: my-sandbox",
			expected: "Stopped sandbox: my-sandbox",
		},
		"message with special characters": {
			message:  "Removed sandbox: test-123",
			expected: "Removed sandbox: test-123",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			var buf bytes.Buffer
			p := printer.NewTablePrinter(&buf)

			err := p.PrintMessage(test.message)
			assert.NoError(err)

			output := strings.TrimSpace(buf.String())
			assert.Equal(test.expected, output)
		})
	}
}

func TestJSONPrinter_PrintList(t *testing.T) {
	createdAt := time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 1, 30, 10, 0, 5, 0, time.UTC)

	tests := map[string]struct {
		sandboxes    []model.Sandbox
		expectedJSON string
	}{
		"empty list": {
			sandboxes:    []model.Sandbox{},
			expectedJSON: `[]`,
		},
		"single sandbox": {
			sandboxes: []model.Sandbox{
				{
					ID:        "01234567890ABCDEFGHIJKLMNOP",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				},
			},
			expectedJSON: `[
  {
    "id": "01234567890ABCDEFGHIJKLMNOP",
    "name": "my-sandbox",
    "status": "running",
    "created_at": "2026-01-30T10:00:00Z"
  }
]`,
		},
		"multiple sandboxes": {
			sandboxes: []model.Sandbox{
				{
					ID:        "01234567890ABCDEFGHIJKLMNOP",
					Name:      "sandbox-1",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
				},
				{
					ID:        "11234567890ABCDEFGHIJKLMNOP",
					Name:      "sandbox-2",
					Status:    model.SandboxStatusStopped,
					CreatedAt: createdAt.Add(-2 * time.Hour),
				},
			},
			expectedJSON: `[
  {
    "id": "01234567890ABCDEFGHIJKLMNOP",
    "name": "sandbox-1",
    "status": "running",
    "created_at": "2026-01-30T10:00:00Z"
  },
  {
    "id": "11234567890ABCDEFGHIJKLMNOP",
    "name": "sandbox-2",
    "status": "stopped",
    "created_at": "2026-01-30T08:00:00Z"
  }
]`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			var buf bytes.Buffer
			p := printer.NewJSONPrinter(&buf)

			err := p.PrintList(test.sandboxes)
			assert.NoError(err)

			output := strings.TrimSpace(buf.String())
			assert.JSONEq(test.expectedJSON, output)
		})
	}
}

func TestJSONPrinter_PrintStatus(t *testing.T) {
	createdAt := time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC)
	startedAt := time.Date(2026, 1, 30, 10, 0, 5, 0, time.UTC)
	stoppedAt := time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		sandbox      model.Sandbox
		expectedJSON string
	}{
		"running sandbox": {
			sandbox: model.Sandbox{
				ID:        "01234567890ABCDEFGHIJKLMNOP",
				Name:      "my-sandbox",
				Status:    model.SandboxStatusRunning,
				CreatedAt: createdAt,
				StartedAt: &startedAt,
				Config: model.SandboxConfig{
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10,
					},
				},
			},
			expectedJSON: `{
  "id": "01234567890ABCDEFGHIJKLMNOP",
  "name": "my-sandbox",
  "status": "running",
  "base": "ubuntu:22.04",
  "vcpus": 2,
  "memory_mb": 2048,
  "disk_gb": 10,
  "created_at": "2026-01-30T10:00:00Z",
  "started_at": "2026-01-30T10:00:05Z",
  "stopped_at": null
}`,
		},
		"stopped sandbox": {
			sandbox: model.Sandbox{
				ID:        "01234567890ABCDEFGHIJKLMNOP",
				Name:      "stopped-sandbox",
				Status:    model.SandboxStatusStopped,
				CreatedAt: createdAt,
				StartedAt: &startedAt,
				StoppedAt: &stoppedAt,
				Config: model.SandboxConfig{
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    4,
						MemoryMB: 4096,
						DiskGB:   20,
					},
				},
			},
			expectedJSON: `{
  "id": "01234567890ABCDEFGHIJKLMNOP",
  "name": "stopped-sandbox",
  "status": "stopped",
  "base": "ubuntu:22.04",
  "vcpus": 4,
  "memory_mb": 4096,
  "disk_gb": 20,
  "created_at": "2026-01-30T10:00:00Z",
  "started_at": "2026-01-30T10:00:05Z",
  "stopped_at": "2026-01-30T12:00:00Z"
}`,
		},
		"failed sandbox with error": {
			sandbox: model.Sandbox{
				ID:        "01234567890ABCDEFGHIJKLMNOP",
				Name:      "failed-sandbox",
				Status:    model.SandboxStatusFailed,
				CreatedAt: createdAt,
				Error:     "failed to provision VM",
				Config: model.SandboxConfig{
					Base: "ubuntu:22.04",
					Resources: model.Resources{
						VCPUs:    2,
						MemoryMB: 2048,
						DiskGB:   10,
					},
				},
			},
			expectedJSON: `{
  "id": "01234567890ABCDEFGHIJKLMNOP",
  "name": "failed-sandbox",
  "status": "failed",
  "base": "ubuntu:22.04",
  "vcpus": 2,
  "memory_mb": 2048,
  "disk_gb": 10,
  "created_at": "2026-01-30T10:00:00Z",
  "started_at": null,
  "stopped_at": null,
  "error": "failed to provision VM"
}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)
			var buf bytes.Buffer
			p := printer.NewJSONPrinter(&buf)

			err := p.PrintStatus(test.sandbox)
			require.NoError(err)

			output := strings.TrimSpace(buf.String())
			assert.JSONEq(test.expectedJSON, output)
		})
	}
}

func TestJSONPrinter_PrintMessage(t *testing.T) {
	tests := map[string]struct {
		message      string
		expectedJSON string
	}{
		"simple message": {
			message:      "Stopped sandbox: my-sandbox",
			expectedJSON: `{"message": "Stopped sandbox: my-sandbox"}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			var buf bytes.Buffer
			p := printer.NewJSONPrinter(&buf)

			err := p.PrintMessage(test.message)
			assert.NoError(err)

			output := strings.TrimSpace(buf.String())
			assert.JSONEq(test.expectedJSON, output)
		})
	}
}
