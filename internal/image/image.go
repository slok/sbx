package image

import (
	"context"
	"io"
	"runtime"

	"github.com/slok/sbx/internal/model"
)

// ImageManager manages image releases (listing, pulling, removing, inspecting).
type ImageManager interface {
	// ListReleases returns available releases, marking installed ones.
	// This includes both remote releases and local snapshot images.
	ListReleases(ctx context.Context) ([]model.ImageRelease, error)
	// GetManifest fetches the full manifest for a specific version.
	// Works for both remote releases and local snapshot images.
	GetManifest(ctx context.Context, version string) (*model.ImageManifest, error)
	// Pull downloads all artifacts for a version to local storage.
	Pull(ctx context.Context, version string, opts PullOptions) (*PullResult, error)
	// Remove deletes a locally installed version.
	Remove(ctx context.Context, version string) error
	// Exists checks if a version is installed locally.
	Exists(ctx context.Context, version string) (bool, error)
	// KernelPath returns the local kernel path for an installed version.
	KernelPath(version string) string
	// RootFSPath returns the local rootfs path for an installed version.
	RootFSPath(version string) string
	// FirecrackerPath returns the local firecracker binary path for an installed version.
	FirecrackerPath(version string) string
	// CreateSnapshot creates a local snapshot image from a sandbox's rootfs and kernel.
	// The name must be unique (no collision with existing images).
	CreateSnapshot(ctx context.Context, opts CreateSnapshotOptions) error
}

// PullOptions configures the pull operation.
type PullOptions struct {
	// Force re-downloads even if already installed.
	Force bool
	// StatusWriter receives progress output during downloads.
	StatusWriter io.Writer
}

// PullResult contains the result of a pull operation.
type PullResult struct {
	Version         string
	Skipped         bool
	KernelPath      string
	RootFSPath      string
	FirecrackerPath string
}

// CreateSnapshotOptions configures snapshot image creation.
type CreateSnapshotOptions struct {
	// Name is the image name for the snapshot.
	Name string
	// KernelSrc is the path to the source kernel binary.
	KernelSrc string
	// RootFSSrc is the path to the source rootfs .ext4 file.
	RootFSSrc string
	// SourceSandboxID is the ULID of the source sandbox.
	SourceSandboxID string
	// SourceSandboxName is the name of the source sandbox.
	SourceSandboxName string
	// SourceImage is the image the source sandbox was created from (if known).
	SourceImage string
	// ParentSnapshot is the snapshot this was derived from (for chains).
	ParentSnapshot string
	// FirecrackerVersion is the firecracker version used (informational).
	FirecrackerVersion string
}

// HostArch returns the Firecracker architecture name for the current host.
func HostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}
