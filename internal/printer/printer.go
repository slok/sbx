package printer

import "github.com/slok/sbx/internal/model"

// Printer knows how to print sandbox information in different formats.
type Printer interface {
	PrintList(sandboxes []model.Sandbox) error
	PrintStatus(sandbox model.Sandbox) error
	PrintImageList(releases []model.ImageRelease) error
	PrintImageInspect(manifest model.ImageManifest) error
	PrintMessage(msg string) error
}
