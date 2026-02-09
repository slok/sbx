package image

import (
	"context"
	"io"
	"runtime"

	"github.com/slok/sbx/internal/model"
)

// ImageManager handles local image operations. It works uniformly for all images
// stored on disk (both pulled releases and snapshot images).
type ImageManager interface {
	// List returns all locally installed images (releases and snapshots).
	List(ctx context.Context) ([]model.ImageRelease, error)
	// GetManifest reads the manifest for a locally installed image.
	GetManifest(ctx context.Context, name string) (*model.ImageManifest, error)
	// Remove deletes a locally installed image.
	Remove(ctx context.Context, name string) error
	// Exists checks if an image is installed locally.
	Exists(ctx context.Context, name string) (bool, error)
	// KernelPath returns the local kernel path for an installed image.
	KernelPath(name string) string
	// RootFSPath returns the local rootfs path for an installed image.
	RootFSPath(name string) string
	// FirecrackerPath returns the local firecracker binary path for an installed image.
	FirecrackerPath(name string) string
}

// ImagePuller downloads remote images to local storage.
type ImagePuller interface {
	// Pull downloads all artifacts for a version to local storage.
	Pull(ctx context.Context, version string, opts PullOptions) (*PullResult, error)
	// ListRemote returns available remote releases (not necessarily installed locally).
	ListRemote(ctx context.Context) ([]model.ImageRelease, error)
}

// SnapshotCreator creates local snapshot images from sandbox files.
type SnapshotCreator interface {
	// Create creates a local snapshot image from a sandbox's rootfs and kernel.
	Create(ctx context.Context, opts CreateSnapshotOptions) error
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
	// FirecrackerSrc is the path to the source firecracker binary (optional, copied if set).
	FirecrackerSrc string
	// SourceSandboxID is the ULID of the source sandbox.
	SourceSandboxID string
	// SourceSandboxName is the name of the source sandbox.
	SourceSandboxName string
	// SourceImage is the image the source sandbox was created from (if known).
	SourceImage string
	// ParentSnapshot is the snapshot this was derived from (for chains).
	ParentSnapshot string
	// SourceManifest is the manifest from the source image (if known). Used to
	// carry over kernel version, rootfs distro info, firecracker info, and build metadata.
	SourceManifest *model.ImageManifest
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
