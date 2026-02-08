package model

// ImageRelease represents a release available in the image registry.
type ImageRelease struct {
	// Version is the release version (e.g. "v0.1.0").
	Version string
	// Installed indicates whether this release is downloaded locally.
	Installed bool
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
