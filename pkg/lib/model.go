package lib

import (
	"io"
	"time"

	"github.com/slok/sbx/internal/model"
)

// EngineType identifies the sandbox engine implementation.
type EngineType string

const (
	// EngineFirecracker uses Firecracker microVMs for real isolated sandboxes.
	// Requires KVM access and appropriate host capabilities.
	EngineFirecracker EngineType = "firecracker"

	// EngineFake uses an in-memory simulation (no real VMs).
	// Use this for unit testing without infrastructure dependencies.
	EngineFake EngineType = "fake"
)

// SandboxStatus represents the lifecycle state of a sandbox.
//
// The typical lifecycle is:
//
//	pending -> created -> running -> stopped -> (removed)
//
// A sandbox can also transition to failed at any point if an error occurs.
type SandboxStatus string

const (
	// SandboxStatusPending indicates the sandbox is being provisioned.
	SandboxStatusPending SandboxStatus = "pending"
	// SandboxStatusCreated indicates the sandbox is provisioned but not started.
	SandboxStatusCreated SandboxStatus = "created"
	// SandboxStatusRunning indicates the sandbox is running and accepting commands.
	SandboxStatusRunning SandboxStatus = "running"
	// SandboxStatusStopped indicates the sandbox is stopped. It can be started again.
	SandboxStatusStopped SandboxStatus = "stopped"
	// SandboxStatusFailed indicates the sandbox encountered an unrecoverable error.
	SandboxStatusFailed SandboxStatus = "failed"
)

// Sandbox represents a sandbox instance returned by the SDK.
//
// This is a read-only snapshot of the sandbox state at the time of the API call.
// Use [Client.GetSandbox] to get the latest state.
type Sandbox struct {
	// ID is the unique identifier (ULID) assigned at creation.
	ID string
	// Name is the human-friendly name.
	Name string
	// Status is the current lifecycle state.
	Status SandboxStatus
	// Config is the static configuration set at creation time.
	Config SandboxConfig
	// CreatedAt is when the sandbox was created.
	CreatedAt time.Time
	// StartedAt is when the sandbox was last started. Nil if never started.
	StartedAt *time.Time
	// StoppedAt is when the sandbox was last stopped. Nil if never stopped.
	StoppedAt *time.Time
}

// SandboxConfig is the immutable configuration of a sandbox, set at creation time.
type SandboxConfig struct {
	// Name is the sandbox name.
	Name string
	// Firecracker holds Firecracker-specific config. Nil for non-Firecracker engines.
	Firecracker *FirecrackerConfig
	// Resources defines the compute resources allocated to the sandbox.
	Resources Resources
}

// FirecrackerConfig contains Firecracker microVM engine-specific settings.
type FirecrackerConfig struct {
	// RootFS is the path to the root filesystem image (ext4).
	RootFS string
	// KernelImage is the path to the kernel binary (vmlinux).
	KernelImage string
}

// Resources defines the compute resources for a sandbox.
type Resources struct {
	// VCPUs is the number of virtual CPUs. Can be fractional (e.g. 0.5).
	VCPUs float64
	// MemoryMB is the memory allocation in megabytes.
	MemoryMB int
	// DiskGB is the disk size in gigabytes.
	DiskGB int
}

// CreateSandboxOpts configures sandbox creation.
//
// Name and Engine are required. For [EngineFirecracker], you must also provide
// Firecracker config with kernel and rootfs paths (unless using FromSnapshot
// or FromImage). Resources must have positive values.
type CreateSandboxOpts struct {
	// Name is the sandbox name (required). Must be unique.
	Name string
	// Engine selects the engine type (required).
	Engine EngineType
	// Firecracker contains engine-specific config. Required for [EngineFirecracker]
	// unless FromSnapshot is set. Ignored for [EngineFake].
	Firecracker *FirecrackerConfig
	// Resources defines compute resources (required, must be positive values).
	Resources Resources
	// FromSnapshot restores the sandbox rootfs from a snapshot name or ID.
	// When set, the Firecracker.RootFS field is overridden with the snapshot path.
	FromSnapshot string
	// FromImage uses a pulled image version (e.g. "v0.1.0") for kernel and rootfs.
	// Cannot be combined with FromSnapshot or explicit Firecracker paths.
	FromImage string
}

// StartSandboxOpts configures sandbox start behavior.
//
// Pass nil to [Client.StartSandbox] to use defaults (no session env).
type StartSandboxOpts struct {
	// Env contains session environment variables injected into the sandbox at
	// start time. These are written to /etc/sbx/session-env.sh and sourced
	// by login shells.
	Env map[string]string
}

// ListSandboxesOpts configures sandbox listing.
//
// Pass nil to [Client.ListSandboxes] to list all sandboxes.
type ListSandboxesOpts struct {
	// Status filters sandboxes by status. Nil means all statuses.
	Status *SandboxStatus
}

// ExecOpts configures command execution inside a sandbox.
//
// Pass nil to [Client.Exec] to use defaults (no working dir, no extra env,
// discarded stdout/stderr).
type ExecOpts struct {
	// WorkingDir sets the working directory for the command inside the sandbox.
	WorkingDir string
	// Env contains additional environment variables for this execution only.
	Env map[string]string
	// Stdin is the standard input stream. Nil means no input.
	Stdin io.Reader
	// Stdout receives the command's standard output. Nil means output is discarded.
	Stdout io.Writer
	// Stderr receives the command's standard error. Nil means output is discarded.
	Stderr io.Writer
	// Tty allocates a pseudo-TTY for the command (useful for interactive shells).
	Tty bool
}

// ExecResult contains the result of a command execution.
type ExecResult struct {
	// ExitCode is the exit status of the executed command.
	// 0 indicates success, non-zero indicates failure.
	ExitCode int
}

// --- Snapshot types ---

// Snapshot represents a point-in-time copy of a sandbox's rootfs.
type Snapshot struct {
	// ID is the unique identifier (ULID).
	ID string
	// Name is the human-friendly name.
	Name string
	// Path is the filesystem path to the snapshot .ext4 file.
	Path string
	// SourceSandboxID is the ID of the sandbox this snapshot was taken from.
	SourceSandboxID string
	// SourceSandboxName is the name of the sandbox this snapshot was taken from.
	SourceSandboxName string
	// VirtualSizeBytes is the logical file size in bytes.
	VirtualSizeBytes int64
	// AllocatedSizeBytes is the actual disk space used (sparse-aware).
	AllocatedSizeBytes int64
	// CreatedAt is when the snapshot was created.
	CreatedAt time.Time
}

// CreateSnapshotOpts configures snapshot creation.
//
// Pass nil to [Client.CreateSnapshot] to auto-generate the snapshot name.
type CreateSnapshotOpts struct {
	// SnapshotName is an optional name for the snapshot.
	// If empty, a name is auto-generated from the sandbox name and timestamp.
	SnapshotName string
}

// --- Image types ---

// ImageRelease represents an image version available in the registry.
type ImageRelease struct {
	// Version is the release version string (e.g. "v0.1.0").
	Version string
	// Installed indicates whether this version is downloaded locally.
	Installed bool
}

// PullImageOpts configures image pull behavior.
//
// Pass nil to [Client.PullImage] to use defaults (no force, no progress output).
type PullImageOpts struct {
	// Force re-downloads the image even if already installed.
	Force bool
	// StatusWriter receives progress output during download. Nil means silent.
	StatusWriter io.Writer
}

// PullResult contains the result of an image pull operation.
type PullResult struct {
	// Version is the pulled image version.
	Version string
	// Skipped is true if the image was already installed and Force was false.
	Skipped bool
	// KernelPath is the local path to the kernel binary.
	KernelPath string
	// RootFSPath is the local path to the rootfs image.
	RootFSPath string
	// FirecrackerPath is the local path to the firecracker binary.
	FirecrackerPath string
}

// ImageManifest describes an image release's artifacts and metadata.
type ImageManifest struct {
	// SchemaVersion is the manifest schema version.
	SchemaVersion int
	// Version is the release version.
	Version string
	// Artifacts maps architecture names (e.g. "x86_64") to their artifacts.
	Artifacts map[string]ArchArtifacts
	// Firecracker describes the expected Firecracker binary version.
	Firecracker FirecrackerInfo
	// Build contains build metadata.
	Build BuildInfo
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

// --- Forward types ---

// PortMapping represents a port forwarding configuration.
type PortMapping struct {
	// LocalPort is the port on the host machine.
	LocalPort int
	// RemotePort is the port inside the sandbox.
	RemotePort int
}

// --- Doctor types ---

// CheckStatus represents the status of a preflight check.
type CheckStatus string

const (
	// CheckStatusOK indicates the check passed.
	CheckStatusOK CheckStatus = "ok"
	// CheckStatusWarning indicates the check passed with a warning.
	CheckStatusWarning CheckStatus = "warning"
	// CheckStatusError indicates the check failed.
	CheckStatusError CheckStatus = "error"
)

// CheckResult represents the result of a single preflight check.
type CheckResult struct {
	// ID is a unique identifier for the check (e.g. "kvm_available").
	ID string
	// Message is a human-readable description of the result.
	Message string
	// Status is the check status.
	Status CheckStatus
}

// --- Internal conversion helpers ---

func toInternalSandboxConfig(opts CreateSandboxOpts) model.SandboxConfig {
	cfg := model.SandboxConfig{
		Name: opts.Name,
		Resources: model.Resources{
			VCPUs:    opts.Resources.VCPUs,
			MemoryMB: opts.Resources.MemoryMB,
			DiskGB:   opts.Resources.DiskGB,
		},
	}

	if opts.Firecracker != nil {
		cfg.FirecrackerEngine = &model.FirecrackerEngineConfig{
			RootFS:      opts.Firecracker.RootFS,
			KernelImage: opts.Firecracker.KernelImage,
		}
	}

	return cfg
}

func toInternalSessionConfig(opts *StartSandboxOpts) model.SessionConfig {
	if opts == nil {
		return model.SessionConfig{}
	}

	return model.SessionConfig{
		Env: opts.Env,
	}
}

func toInternalExecOpts(opts *ExecOpts) model.ExecOpts {
	if opts == nil {
		return model.ExecOpts{}
	}

	return model.ExecOpts{
		WorkingDir: opts.WorkingDir,
		Env:        opts.Env,
		Stdin:      opts.Stdin,
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
		Tty:        opts.Tty,
	}
}

func fromInternalSandbox(s model.Sandbox) Sandbox {
	sb := Sandbox{
		ID:        s.ID,
		Name:      s.Name,
		Status:    SandboxStatus(s.Status),
		CreatedAt: s.CreatedAt,
		StartedAt: s.StartedAt,
		StoppedAt: s.StoppedAt,
		Config: SandboxConfig{
			Name: s.Config.Name,
			Resources: Resources{
				VCPUs:    s.Config.Resources.VCPUs,
				MemoryMB: s.Config.Resources.MemoryMB,
				DiskGB:   s.Config.Resources.DiskGB,
			},
		},
	}

	if s.Config.FirecrackerEngine != nil {
		sb.Config.Firecracker = &FirecrackerConfig{
			RootFS:      s.Config.FirecrackerEngine.RootFS,
			KernelImage: s.Config.FirecrackerEngine.KernelImage,
		}
	}

	return sb
}

func fromInternalSandboxList(ss []model.Sandbox) []Sandbox {
	result := make([]Sandbox, len(ss))
	for i, s := range ss {
		result[i] = fromInternalSandbox(s)
	}
	return result
}

func toInternalStatusFilter(opts *ListSandboxesOpts) *model.SandboxStatus {
	if opts == nil || opts.Status == nil {
		return nil
	}
	s := model.SandboxStatus(*opts.Status)
	return &s
}

func mapError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case isInternalError(err, model.ErrNotFound):
		return joinErrors(err, ErrNotFound)
	case isInternalError(err, model.ErrAlreadyExists):
		return joinErrors(err, ErrAlreadyExists)
	case isInternalError(err, model.ErrNotValid):
		return joinErrors(err, ErrNotValid)
	default:
		return err
	}
}

func isInternalError(err, target error) bool {
	for {
		if err == target {
			return true
		}
		unwrapped := unwrapSingle(err)
		if unwrapped == nil {
			return false
		}
		err = unwrapped
	}
}

func unwrapSingle(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}

func joinErrors(original, sentinel error) error {
	return &mappedError{original: original, sentinel: sentinel}
}

type mappedError struct {
	original error
	sentinel error
}

func (e *mappedError) Error() string { return e.original.Error() }

func (e *mappedError) Is(target error) bool {
	return target == e.sentinel
}

func (e *mappedError) Unwrap() error { return e.original }

// --- Snapshot conversion helpers ---

func fromInternalSnapshot(s model.Snapshot) Snapshot {
	return Snapshot{
		ID:                 s.ID,
		Name:               s.Name,
		Path:               s.Path,
		SourceSandboxID:    s.SourceSandboxID,
		SourceSandboxName:  s.SourceSandboxName,
		VirtualSizeBytes:   s.VirtualSizeBytes,
		AllocatedSizeBytes: s.AllocatedSizeBytes,
		CreatedAt:          s.CreatedAt,
	}
}

func fromInternalSnapshotList(ss []model.Snapshot) []Snapshot {
	result := make([]Snapshot, len(ss))
	for i, s := range ss {
		result[i] = fromInternalSnapshot(s)
	}
	return result
}

// --- Image conversion helpers ---

func fromInternalImageRelease(r model.ImageRelease) ImageRelease {
	return ImageRelease{
		Version:   r.Version,
		Installed: r.Installed,
	}
}

func fromInternalImageReleaseList(rs []model.ImageRelease) []ImageRelease {
	result := make([]ImageRelease, len(rs))
	for i, r := range rs {
		result[i] = fromInternalImageRelease(r)
	}
	return result
}

func fromInternalImageManifest(m *model.ImageManifest) *ImageManifest {
	if m == nil {
		return nil
	}

	artifacts := make(map[string]ArchArtifacts, len(m.Artifacts))
	for arch, a := range m.Artifacts {
		artifacts[arch] = ArchArtifacts{
			Kernel: KernelInfo{
				File:      a.Kernel.File,
				Version:   a.Kernel.Version,
				Source:    a.Kernel.Source,
				SizeBytes: a.Kernel.SizeBytes,
			},
			Rootfs: RootfsInfo{
				File:          a.Rootfs.File,
				Distro:        a.Rootfs.Distro,
				DistroVersion: a.Rootfs.DistroVersion,
				Profile:       a.Rootfs.Profile,
				SizeBytes:     a.Rootfs.SizeBytes,
			},
		}
	}

	return &ImageManifest{
		SchemaVersion: m.SchemaVersion,
		Version:       m.Version,
		Artifacts:     artifacts,
		Firecracker: FirecrackerInfo{
			Version: m.Firecracker.Version,
			Source:  m.Firecracker.Source,
		},
		Build: BuildInfo{
			Date:   m.Build.Date,
			Commit: m.Build.Commit,
		},
	}
}

// --- Forward conversion helpers ---

func toInternalPortMappings(ports []PortMapping) []model.PortMapping {
	result := make([]model.PortMapping, len(ports))
	for i, p := range ports {
		result[i] = model.PortMapping{
			LocalPort:  p.LocalPort,
			RemotePort: p.RemotePort,
		}
	}
	return result
}

// --- Doctor conversion helpers ---

func fromInternalCheckResults(results []model.CheckResult) []CheckResult {
	out := make([]CheckResult, len(results))
	for i, r := range results {
		out[i] = CheckResult{
			ID:      r.ID,
			Message: r.Message,
			Status:  CheckStatus(r.Status),
		}
	}
	return out
}
