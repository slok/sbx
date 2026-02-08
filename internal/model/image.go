package model

import (
	"fmt"
	"regexp"
	"time"
)

// ImageSource indicates where an image comes from.
type ImageSource string

const (
	// ImageSourceRelease is a remote image from GitHub releases.
	ImageSourceRelease ImageSource = "release"
	// ImageSourceSnapshot is a local image created from a sandbox snapshot.
	ImageSourceSnapshot ImageSource = "snapshot"
)

// ImageRelease represents a release available in the image registry.
type ImageRelease struct {
	// Version is the release version (e.g. "v0.1.0") or snapshot name.
	Version string
	// Installed indicates whether this release is downloaded locally.
	Installed bool
	// Source indicates where this image comes from (release or snapshot).
	Source ImageSource
}

// CurrentSchemaVersion is the manifest schema version supported by this client.
const CurrentSchemaVersion = 1

// ImageManifest is the full manifest for an image release, describing all
// artifacts, the expected Firecracker version, and build metadata.
type ImageManifest struct {
	SchemaVersion int
	Version       string
	Artifacts     map[string]ArchArtifacts // keyed by arch (e.g. "x86_64").
	Firecracker   FirecrackerInfo
	Build         BuildInfo
	// Snapshot contains snapshot-specific metadata (nil for release images).
	Snapshot *SnapshotInfo
}

// ArchArtifacts contains per-architecture artifact metadata.
type ArchArtifacts struct {
	Kernel KernelInfo
	Rootfs RootfsInfo
}

// KernelInfo describes the kernel binary artifact.
type KernelInfo struct {
	File      string
	Version   string
	Source    string
	SizeBytes int64
}

// RootfsInfo describes the rootfs image artifact.
type RootfsInfo struct {
	File          string
	Distro        string
	DistroVersion string
	Profile       string
	SizeBytes     int64
}

// FirecrackerInfo describes the expected Firecracker version.
type FirecrackerInfo struct {
	Version string
	Source  string
}

// BuildInfo contains build metadata.
type BuildInfo struct {
	Date   string
	Commit string
}

// SnapshotInfo contains metadata specific to snapshot-created images.
type SnapshotInfo struct {
	// SourceSandboxID is the ULID of the sandbox this snapshot was taken from.
	SourceSandboxID string
	// SourceSandboxName is the name of the source sandbox.
	SourceSandboxName string
	// SourceImage is the image version the source sandbox was created from (if known).
	SourceImage string
	// ParentSnapshot is the snapshot image name this was derived from (for snapshot chains).
	ParentSnapshot string
	// CreatedAt is when the snapshot was created.
	CreatedAt time.Time
}

var imageNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidateImageName validates an image name (used for snapshot-created images).
func ValidateImageName(name string) error {
	if name == "" {
		return fmt.Errorf("image name is required: %w", ErrNotValid)
	}

	if !imageNameRegexp.MatchString(name) {
		return fmt.Errorf("image name %q is invalid (allowed: [a-zA-Z0-9._-]): %w", name, ErrNotValid)
	}

	return nil
}
