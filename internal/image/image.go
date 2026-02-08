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
	ListReleases(ctx context.Context) ([]model.ImageRelease, error)
	// GetManifest fetches the full manifest for a specific version.
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
