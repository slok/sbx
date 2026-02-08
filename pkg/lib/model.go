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
	// FirecrackerBinary is the path to the firecracker binary.
	// If empty, the binary is searched in ./bin/ and PATH.
	FirecrackerBinary string
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
