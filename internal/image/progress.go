package image

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// ProgressWriter wraps an io.Writer to display download progress.
type ProgressWriter struct {
	dst          io.Writer
	statusWriter io.Writer
	total        int64
	written      int64
	mu           sync.Mutex
}

// NewProgressWriter creates a new progress writer.
// dst receives the actual data, statusWriter receives progress output.
// If total is 0 or negative, only bytes written are shown (no percentage).
func NewProgressWriter(dst io.Writer, statusWriter io.Writer, total int64) *ProgressWriter {
	return &ProgressWriter{
		dst:          dst,
		statusWriter: statusWriter,
		total:        total,
	}
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.dst.Write(p)

	pw.mu.Lock()
	pw.written += int64(n)
	pw.printProgress()
	pw.mu.Unlock()

	return n, err
}

// Finish prints the final progress line with a newline.
func (pw *ProgressWriter) Finish() {
	fmt.Fprintln(pw.statusWriter)
}

func (pw *ProgressWriter) printProgress() {
	if pw.total > 0 {
		pct := float64(pw.written) / float64(pw.total) * 100
		barWidth := 40
		filled := int(pct / 100 * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		fmt.Fprintf(pw.statusWriter, "\r  [%s] %3.0f%% %s / %s", bar, pct, formatSize(pw.written), formatSize(pw.total))
	} else {
		fmt.Fprintf(pw.statusWriter, "\r  %s downloaded", formatSize(pw.written))
	}
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
