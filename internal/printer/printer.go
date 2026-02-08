package printer

import "github.com/slok/sbx/internal/model"

// Printer knows how to print sandbox information in different formats.
type Printer interface {
	// PrintList prints a list of sandboxes.
	PrintList(sandboxes []model.Sandbox) error
	// PrintStatus prints detailed status of a single sandbox.
	PrintStatus(sandbox model.Sandbox) error
	// PrintSnapshotList prints a list of snapshots.
	PrintSnapshotList(snapshots []model.Snapshot) error
	// PrintImageList prints a list of image releases.
	PrintImageList(releases []model.ImageRelease) error
	// PrintImageInspect prints detailed image manifest information.
	PrintImageInspect(manifest model.ImageManifest) error
	// PrintMessage prints a simple text message.
	PrintMessage(msg string) error
}
