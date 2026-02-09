package image

import (
	"context"
	"io"
	"runtime"

	"github.com/slok/sbx/internal/model"
)

// ImageManager manages remote image releases (listing, pulling, removing, inspecting).
type ImageManager interface {
	// ListReleases returns available remote releases, marking installed ones.
	ListReleases(ctx context.Context) ([]model.ImageRelease, error)
	// GetManifest fetches the manifest for a remote release version.
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
}

// SnapshotManager manages local snapshot images (CRUD operations on the filesystem).
type SnapshotManager interface {
	// Create creates a local snapshot image from a sandbox's rootfs and kernel.
	Create(ctx context.Context, opts CreateSnapshotOptions) error
	// List returns all local snapshot images.
	List(ctx context.Context) ([]model.ImageRelease, error)
	// GetManifest reads the manifest for a local snapshot image.
	GetManifest(ctx context.Context, name string) (*model.ImageManifest, error)
	// Remove deletes a local snapshot image.
	Remove(ctx context.Context, name string) error
	// Exists checks if a snapshot image exists locally.
	Exists(ctx context.Context, name string) (bool, error)
	// KernelPath returns the local kernel path for a snapshot image.
	KernelPath(name string) string
	// RootFSPath returns the local rootfs path for a snapshot image.
	RootFSPath(name string) string
	// FirecrackerPath returns the local firecracker binary path for a snapshot image.
	FirecrackerPath(name string) string
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
