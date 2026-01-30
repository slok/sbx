package printer

import (
	"encoding/json"
	"io"
	"time"

	"github.com/slok/sbx/internal/model"
)

// JSONPrinter prints sandbox information in JSON format.
type JSONPrinter struct {
	writer io.Writer
}

// NewJSONPrinter creates a new JSON printer.
func NewJSONPrinter(w io.Writer) *JSONPrinter {
	return &JSONPrinter{writer: w}
}

// listItem represents a sandbox in the list output (subset of fields).
type listItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// statusOutput represents the full sandbox status output.
type statusOutput struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Base      string     `json:"base"`
	VCPUs     int        `json:"vcpus"`
	MemoryMB  int        `json:"memory_mb"`
	DiskGB    int        `json:"disk_gb"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at"`
	StoppedAt *time.Time `json:"stopped_at"`
	Error     string     `json:"error,omitempty"`
}

// messageOutput represents a simple message output.
type messageOutput struct {
	Message string `json:"message"`
}

// PrintList prints sandboxes in JSON format with a subset of fields.
func (j *JSONPrinter) PrintList(sandboxes []model.Sandbox) error {
	items := make([]listItem, len(sandboxes))
	for i, s := range sandboxes {
		items[i] = listItem{
			ID:        s.ID,
			Name:      s.Name,
			Status:    string(s.Status),
			CreatedAt: s.CreatedAt.UTC(),
		}
	}

	enc := json.NewEncoder(j.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

// PrintStatus prints detailed sandbox status in JSON format.
func (j *JSONPrinter) PrintStatus(sandbox model.Sandbox) error {
	output := statusOutput{
		ID:        sandbox.ID,
		Name:      sandbox.Name,
		Status:    string(sandbox.Status),
		Base:      sandbox.Config.Base,
		VCPUs:     sandbox.Config.Resources.VCPUs,
		MemoryMB:  sandbox.Config.Resources.MemoryMB,
		DiskGB:    sandbox.Config.Resources.DiskGB,
		CreatedAt: sandbox.CreatedAt.UTC(),
		StartedAt: nil,
		StoppedAt: nil,
		Error:     sandbox.Error,
	}

	if sandbox.StartedAt != nil {
		utcTime := sandbox.StartedAt.UTC()
		output.StartedAt = &utcTime
	}

	if sandbox.StoppedAt != nil {
		utcTime := sandbox.StoppedAt.UTC()
		output.StoppedAt = &utcTime
	}

	enc := json.NewEncoder(j.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// PrintMessage prints a simple message in JSON format.
func (j *JSONPrinter) PrintMessage(msg string) error {
	output := messageOutput{Message: msg}
	enc := json.NewEncoder(j.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}
