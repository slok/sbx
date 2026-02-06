package printer

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/slok/sbx/internal/model"
)

// TablePrinter prints sandbox information in a table format.
type TablePrinter struct {
	writer io.Writer
}

// NewTablePrinter creates a new table printer.
func NewTablePrinter(w io.Writer) *TablePrinter {
	return &TablePrinter{writer: w}
}

// PrintList prints sandboxes in a table format.
func (t *TablePrinter) PrintList(sandboxes []model.Sandbox) error {
	if len(sandboxes) == 0 {
		return nil
	}

	tw := tabwriter.NewWriter(t.writer, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	// Print header
	fmt.Fprintln(tw, "NAME\tSTATUS\tCREATED")

	// Print rows
	for _, s := range sandboxes {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Status, TimeAgo(s.CreatedAt))
	}

	return nil
}

// PrintStatus prints detailed sandbox status.
func (t *TablePrinter) PrintStatus(sandbox model.Sandbox) error {
	fmt.Fprintf(t.writer, "Name:       %s\n", sandbox.Name)
	fmt.Fprintf(t.writer, "ID:         %s\n", sandbox.ID)
	fmt.Fprintf(t.writer, "Status:     %s\n", sandbox.Status)

	// Print engine-specific info
	if sandbox.Config.FirecrackerEngine != nil {
		fmt.Fprintf(t.writer, "Engine:     firecracker\n")
		fmt.Fprintf(t.writer, "RootFS:     %s\n", sandbox.Config.FirecrackerEngine.RootFS)
		fmt.Fprintf(t.writer, "Kernel:     %s\n", sandbox.Config.FirecrackerEngine.KernelImage)
	}

	fmt.Fprintf(t.writer, "VCPUs:      %.2f\n", sandbox.Config.Resources.VCPUs)
	fmt.Fprintf(t.writer, "Memory:     %d MB\n", sandbox.Config.Resources.MemoryMB)
	fmt.Fprintf(t.writer, "Disk:       %d GB\n", sandbox.Config.Resources.DiskGB)
	fmt.Fprintf(t.writer, "Created:    %s\n", FormatTimestamp(sandbox.CreatedAt))

	if sandbox.StartedAt != nil {
		fmt.Fprintf(t.writer, "Started:    %s\n", FormatTimestamp(*sandbox.StartedAt))
	}

	if sandbox.StoppedAt != nil {
		fmt.Fprintf(t.writer, "Stopped:    %s\n", FormatTimestamp(*sandbox.StoppedAt))
	}

	return nil
}

// PrintMessage prints a simple text message.
func (t *TablePrinter) PrintMessage(msg string) error {
	fmt.Fprintln(t.writer, msg)
	return nil
}
