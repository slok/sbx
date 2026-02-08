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
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Engine    *engineOutput `json:"engine,omitempty"`
	VCPUs     float64       `json:"vcpus"`
	MemoryMB  int           `json:"memory_mb"`
	DiskGB    int           `json:"disk_gb"`
	CreatedAt time.Time     `json:"created_at"`
	StartedAt *time.Time    `json:"started_at"`
	StoppedAt *time.Time    `json:"stopped_at"`
}

// engineOutput represents engine configuration output.
type engineOutput struct {
	Type        string `json:"type"`
	RootFS      string `json:"root_fs,omitempty"`
	KernelImage string `json:"kernel_image,omitempty"`
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
		VCPUs:     sandbox.Config.Resources.VCPUs,
		MemoryMB:  sandbox.Config.Resources.MemoryMB,
		DiskGB:    sandbox.Config.Resources.DiskGB,
		CreatedAt: sandbox.CreatedAt.UTC(),
		StartedAt: nil,
		StoppedAt: nil,
	}

	// Add engine info
	if sandbox.Config.FirecrackerEngine != nil {
		output.Engine = &engineOutput{
			Type:        "firecracker",
			RootFS:      sandbox.Config.FirecrackerEngine.RootFS,
			KernelImage: sandbox.Config.FirecrackerEngine.KernelImage,
		}
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

// snapshotListItem represents a snapshot in the list output.
type snapshotListItem struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	SourceSandboxID    string    `json:"source_sandbox_id"`
	SourceSandboxName  string    `json:"source_sandbox_name"`
	VirtualSizeBytes   int64     `json:"virtual_size_bytes"`
	AllocatedSizeBytes int64     `json:"allocated_size_bytes"`
	CreatedAt          time.Time `json:"created_at"`
}

// PrintSnapshotList prints snapshots in JSON format.
func (j *JSONPrinter) PrintSnapshotList(snapshots []model.Snapshot) error {
	items := make([]snapshotListItem, len(snapshots))
	for i, s := range snapshots {
		items[i] = snapshotListItem{
			ID:                 s.ID,
			Name:               s.Name,
			SourceSandboxID:    s.SourceSandboxID,
			SourceSandboxName:  s.SourceSandboxName,
			VirtualSizeBytes:   s.VirtualSizeBytes,
			AllocatedSizeBytes: s.AllocatedSizeBytes,
			CreatedAt:          s.CreatedAt.UTC(),
		}
	}

	enc := json.NewEncoder(j.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

// imageReleaseItem represents an image release in JSON output.
type imageReleaseItem struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

// imageManifestOutput represents a full image manifest in JSON output.
type imageManifestOutput struct {
	Version     string                         `json:"version"`
	Artifacts   map[string]archArtifactsOutput `json:"artifacts"`
	Firecracker firecrackerInfoOutput          `json:"firecracker"`
	Build       buildInfoOutput                `json:"build"`
}

type archArtifactsOutput struct {
	Kernel kernelInfoOutput `json:"kernel"`
	Rootfs rootfsInfoOutput `json:"rootfs"`
}

type kernelInfoOutput struct {
	File      string `json:"file"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	SizeBytes int64  `json:"size_bytes"`
}

type rootfsInfoOutput struct {
	File          string `json:"file"`
	Distro        string `json:"distro"`
	DistroVersion string `json:"distro_version"`
	Profile       string `json:"profile"`
	SizeBytes     int64  `json:"size_bytes"`
}

type firecrackerInfoOutput struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

type buildInfoOutput struct {
	Date   string `json:"date"`
	Commit string `json:"commit"`
}

// PrintImageList prints image releases in JSON format.
func (j *JSONPrinter) PrintImageList(releases []model.ImageRelease) error {
	items := make([]imageReleaseItem, len(releases))
	for i, r := range releases {
		items[i] = imageReleaseItem{
			Version:   r.Version,
			Installed: r.Installed,
		}
	}

	enc := json.NewEncoder(j.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

// PrintImageInspect prints detailed image manifest in JSON format.
func (j *JSONPrinter) PrintImageInspect(manifest model.ImageManifest) error {
	artifacts := make(map[string]archArtifactsOutput, len(manifest.Artifacts))
	for arch, a := range manifest.Artifacts {
		artifacts[arch] = archArtifactsOutput{
			Kernel: kernelInfoOutput{
				File:      a.Kernel.File,
				Version:   a.Kernel.Version,
				Source:    a.Kernel.Source,
				SizeBytes: a.Kernel.SizeBytes,
			},
			Rootfs: rootfsInfoOutput{
				File:          a.Rootfs.File,
				Distro:        a.Rootfs.Distro,
				DistroVersion: a.Rootfs.DistroVersion,
				Profile:       a.Rootfs.Profile,
				SizeBytes:     a.Rootfs.SizeBytes,
			},
		}
	}

	output := imageManifestOutput{
		Version:   manifest.Version,
		Artifacts: artifacts,
		Firecracker: firecrackerInfoOutput{
			Version: manifest.Firecracker.Version,
			Source:  manifest.Firecracker.Source,
		},
		Build: buildInfoOutput{
			Date:   manifest.Build.Date,
			Commit: manifest.Build.Commit,
		},
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
