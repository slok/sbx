package lib

import (
	"io"
	"time"

	"github.com/slok/sbx/internal/model"
)

// EngineType identifies the sandbox engine implementation.
type EngineType string

const (
	// EngineFirecracker uses Firecracker microVMs.
	EngineFirecracker EngineType = "firecracker"
	// EngineFake uses an in-memory fake engine (for testing).
	EngineFake EngineType = "fake"
)

// SandboxStatus represents the lifecycle state of a sandbox.
type SandboxStatus string

const (
	SandboxStatusPending SandboxStatus = "pending"
	SandboxStatusCreated SandboxStatus = "created"
	SandboxStatusRunning SandboxStatus = "running"
	SandboxStatusStopped SandboxStatus = "stopped"
	SandboxStatusFailed  SandboxStatus = "failed"
)

// Sandbox represents a sandbox instance.
type Sandbox struct {
	ID        string
	Name      string
	Status    SandboxStatus
	Config    SandboxConfig
	CreatedAt time.Time
	StartedAt *time.Time
	StoppedAt *time.Time
}

// SandboxConfig is the static configuration of a sandbox.
type SandboxConfig struct {
	Name        string
	Firecracker *FirecrackerConfig
	Resources   Resources
}

// FirecrackerConfig contains Firecracker engine-specific settings.
type FirecrackerConfig struct {
	RootFS      string
	KernelImage string
}

// Resources defines compute resources for a sandbox.
type Resources struct {
	VCPUs    float64
	MemoryMB int
	DiskGB   int
}

// CreateSandboxOpts configures sandbox creation.
type CreateSandboxOpts struct {
	// Name is the sandbox name (required).
	Name string
	// Engine selects the engine type (required).
	Engine EngineType
	// Firecracker contains engine-specific config (required for firecracker engine).
	Firecracker *FirecrackerConfig
	// Resources defines compute resources.
	Resources Resources
	// FromSnapshot creates the sandbox from a snapshot name or ID (optional).
	FromSnapshot string
	// FromImage uses a pulled image version, e.g. "v0.1.0" (optional).
	FromImage string
}

// StartSandboxOpts configures sandbox start.
type StartSandboxOpts struct {
	// Env contains session environment variables applied at start time.
	Env map[string]string
}

// ListSandboxesOpts configures sandbox listing.
type ListSandboxesOpts struct {
	// Status filters sandboxes by status. Nil means all statuses.
	Status *SandboxStatus
}

// ExecOpts configures command execution inside a sandbox.
type ExecOpts struct {
	// WorkingDir sets the working directory for the command.
	WorkingDir string
	// Env contains additional environment variables.
	Env map[string]string
	// Stdin is the input stream (optional).
	Stdin io.Reader
	// Stdout is the output stream (optional).
	Stdout io.Writer
	// Stderr is the error stream (optional).
	Stderr io.Writer
	// Tty allocates a pseudo-TTY.
	Tty bool
}

// ExecResult contains the result of a command execution.
type ExecResult struct {
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
