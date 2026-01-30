package printer

import "github.com/slok/sbx/internal/model"

// Printer knows how to print sandbox information in different formats.
type Printer interface {
	// PrintList prints a list of sandboxes.
	PrintList(sandboxes []model.Sandbox) error
	// PrintStatus prints detailed status of a single sandbox.
	PrintStatus(sandbox model.Sandbox) error
	// PrintMessage prints a simple text message.
	PrintMessage(msg string) error
}
