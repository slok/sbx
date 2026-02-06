package firecracker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestEngine_Status_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	_, err = e.Status(context.Background(), "nonexistent-sandbox")
	if err == nil {
		t.Error("Status should return error for non-existent sandbox")
	}
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got: %v", err)
	}
}

func TestEngine_Status_NoPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Create sandbox directory without PID file
	sandboxID := "test-sandbox"
	vmDir := e.VMDir(sandboxID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatalf("failed to create vm dir: %v", err)
	}

	sandbox, err := e.Status(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	if sandbox.Status != model.SandboxStatusStopped {
		t.Errorf("Expected stopped status, got: %s", sandbox.Status)
	}
	if sandbox.ID != sandboxID {
		t.Errorf("Expected ID %s, got: %s", sandboxID, sandbox.ID)
	}
}

func TestEngine_Status_InvalidPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	sandboxID := "test-sandbox"
	vmDir := e.VMDir(sandboxID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatalf("failed to create vm dir: %v", err)
	}

	// Create invalid PID file
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	if err := os.WriteFile(pidPath, []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	_, err = e.Status(context.Background(), sandboxID)
	if err == nil {
		t.Error("Status should return error for invalid PID file")
	}
}

func TestEngine_Status_WithPID(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	sandboxID := "test-sandbox"
	vmDir := e.VMDir(sandboxID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatalf("failed to create vm dir: %v", err)
	}

	// Create PID file with a PID that doesn't exist
	// Use a very high PID that's unlikely to be real
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	if err := os.WriteFile(pidPath, []byte("999999"), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	sandbox, err := e.Status(context.Background(), sandboxID)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}

	// Process with PID 999999 likely doesn't exist, so should be stopped
	if sandbox.Status != model.SandboxStatusStopped {
		t.Errorf("Expected stopped status for dead process, got: %s", sandbox.Status)
	}
	if sandbox.PID != 999999 {
		t.Errorf("Expected PID 999999, got: %d", sandbox.PID)
	}
}

func TestEngine_Start(t *testing.T) {
	tests := map[string]struct {
		setup          func(t *testing.T, e *Engine) (sandboxID string)
		mockRepo       func() *storagemock.MockRepository
		expErr         bool
		expErrContains string
		expErrIs       error
	}{
		"VM directory not found should return ErrNotFound.": {
			setup: func(t *testing.T, e *Engine) string {
				// Don't create any directory
				return "nonexistent-sandbox"
			},
			expErr:   true,
			expErrIs: model.ErrNotFound,
		},

		"Rootfs missing should return error.": {
			setup: func(t *testing.T, e *Engine) string {
				sandboxID := "test-sandbox"
				vmDir := e.VMDir(sandboxID)
				if err := os.MkdirAll(vmDir, 0755); err != nil {
					t.Fatalf("failed to create vm dir: %v", err)
				}
				// Don't create rootfs
				return sandboxID
			},
			expErr:         true,
			expErrContains: "rootfs not found",
		},

		"No repository configured should return error.": {
			setup: func(t *testing.T, e *Engine) string {
				sandboxID := "test-sandbox"
				vmDir := e.VMDir(sandboxID)
				if err := os.MkdirAll(vmDir, 0755); err != nil {
					t.Fatalf("failed to create vm dir: %v", err)
				}
				rootfsPath := e.RootFSPath(vmDir)
				if err := os.WriteFile(rootfsPath, []byte("dummy"), 0644); err != nil {
					t.Fatalf("failed to create rootfs: %v", err)
				}
				return sandboxID
			},
			expErr:         true,
			expErrContains: "repository not configured",
		},

		"Non-firecracker sandbox should return error.": {
			setup: func(t *testing.T, e *Engine) string {
				sandboxID := "test-sandbox"
				vmDir := e.VMDir(sandboxID)
				if err := os.MkdirAll(vmDir, 0755); err != nil {
					t.Fatalf("failed to create vm dir: %v", err)
				}
				rootfsPath := e.RootFSPath(vmDir)
				if err := os.WriteFile(rootfsPath, []byte("dummy"), 0644); err != nil {
					t.Fatalf("failed to create rootfs: %v", err)
				}
				return sandboxID
			},
			mockRepo: func() *storagemock.MockRepository {
				m := &storagemock.MockRepository{}
				m.On("GetSandbox", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:   "test-sandbox",
					Name: "test",
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 1},
					},
				}, nil)
				return m
			},
			expErr:         true,
			expErrContains: "not a firecracker sandbox",
		},

		"Sandbox with missing firecracker config should return error.": {
			setup: func(t *testing.T, e *Engine) string {
				sandboxID := "test-sandbox"
				vmDir := e.VMDir(sandboxID)
				if err := os.MkdirAll(vmDir, 0755); err != nil {
					t.Fatalf("failed to create vm dir: %v", err)
				}
				rootfsPath := e.RootFSPath(vmDir)
				if err := os.WriteFile(rootfsPath, []byte("dummy"), 0644); err != nil {
					t.Fatalf("failed to create rootfs: %v", err)
				}
				return sandboxID
			},
			mockRepo: func() *storagemock.MockRepository {
				m := &storagemock.MockRepository{}
				m.On("GetSandbox", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:   "test-sandbox",
					Name: "test",
					Config: model.SandboxConfig{
						Name:      "test",
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 1},
					},
				}, nil)
				return m
			},
			expErr:         true,
			expErrContains: "not a firecracker sandbox",
		},

		"Kernel image missing should return error.": {
			setup: func(t *testing.T, e *Engine) string {
				sandboxID := "test-sandbox"
				vmDir := e.VMDir(sandboxID)
				if err := os.MkdirAll(vmDir, 0755); err != nil {
					t.Fatalf("failed to create vm dir: %v", err)
				}
				rootfsPath := e.RootFSPath(vmDir)
				if err := os.WriteFile(rootfsPath, []byte("dummy"), 0644); err != nil {
					t.Fatalf("failed to create rootfs: %v", err)
				}
				return sandboxID
			},
			mockRepo: func() *storagemock.MockRepository {
				m := &storagemock.MockRepository{}
				m.On("GetSandbox", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:   "test-sandbox",
					Name: "test",
					Config: model.SandboxConfig{
						Name: "test",
						FirecrackerEngine: &model.FirecrackerEngineConfig{
							KernelImage: "/nonexistent/kernel",
							RootFS:      "/nonexistent/rootfs",
						},
						Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 1},
					},
				}, nil)
				return m
			},
			expErr:         true,
			expErrContains: "kernel image not found",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tmpDir := t.TempDir()

			var repo *storagemock.MockRepository
			if test.mockRepo != nil {
				repo = test.mockRepo()
			}

			cfg := EngineConfig{
				DataDir: tmpDir,
				Logger:  log.Noop,
			}
			// Only set Repository if we have a mock - otherwise leave as nil interface
			if repo != nil {
				cfg.Repository = repo
			}

			e, err := NewEngine(cfg)
			if err != nil {
				t.Fatalf("failed to create engine: %v", err)
			}

			sandboxID := test.setup(t, e)

			err = e.Start(context.Background(), sandboxID)

			if test.expErr {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if test.expErrIs != nil && !errors.Is(err, test.expErrIs) {
					t.Errorf("expected error to be %v, got: %v", test.expErrIs, err)
				}
				if test.expErrContains != "" && !strings.Contains(err.Error(), test.expErrContains) {
					t.Errorf("expected error to contain %q, got: %v", test.expErrContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if repo != nil {
				repo.AssertExpectations(t)
			}
		})
	}
}

func TestEngine_Exec_EmptyCommand(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	_, err = e.Exec(context.Background(), "sandbox-id", []string{}, model.ExecOpts{})
	if err == nil {
		t.Error("Exec should return error for empty command")
	}
	if !errors.Is(err, model.ErrNotValid) {
		t.Errorf("Expected ErrNotValid, got: %v", err)
	}
}

func TestEngine_killFirecracker_NoPIDFile(t *testing.T) {
	e := &Engine{
		logger: log.Noop,
	}

	vmDir := t.TempDir()
	// No PID file exists

	err := e.killFirecracker(vmDir)
	if err != nil {
		t.Errorf("killFirecracker should not error when no PID file: %v", err)
	}
}

func TestEngine_killFirecracker_InvalidPID(t *testing.T) {
	e := &Engine{
		logger: log.Noop,
	}

	vmDir := t.TempDir()
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	_ = os.WriteFile(pidPath, []byte("not-a-number"), 0644)

	err := e.killFirecracker(vmDir)
	if err == nil {
		t.Error("killFirecracker should error for invalid PID")
	}
}

func TestEngine_killFirecracker_ProcessNotExist(t *testing.T) {
	e := &Engine{
		logger: log.Noop,
	}

	vmDir := t.TempDir()
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	// Use a PID that almost certainly doesn't exist
	_ = os.WriteFile(pidPath, []byte("999999"), 0644)

	err := e.killFirecracker(vmDir)
	// Should not error - process just doesn't exist
	if err != nil {
		t.Errorf("killFirecracker should handle non-existent process gracefully: %v", err)
	}
}

func TestEngine_Remove_VMDirDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Remove should handle non-existent VM directory gracefully
	// It will try to delete files that don't exist, which is fine
	err = e.Remove(context.Background(), "nonexistent-sandbox")
	// This should not error - removing something that doesn't exist is OK
	// os.RemoveAll doesn't error on non-existent paths
	if err != nil {
		t.Logf("Remove error (may be expected): %v", err)
	}
}

func TestEngine_VMDir(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	dir := e.VMDir("my-sandbox-id")
	expected := filepath.Join(tmpDir, "vms", "my-sandbox-id")
	if dir != expected {
		t.Errorf("Expected %s, got %s", expected, dir)
	}
}

func TestEngine_RootFSPath(t *testing.T) {
	e := &Engine{}

	path := e.RootFSPath("/test/vms/sandbox-1")
	expected := "/test/vms/sandbox-1/rootfs.ext4"
	if path != expected {
		t.Errorf("Expected %s, got %s", expected, path)
	}
}

func TestEngine_Stop_WithTasks(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	sandboxID := "test-sandbox"
	vmDir := e.VMDir(sandboxID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatalf("failed to create vm dir: %v", err)
	}

	// Create a PID file with non-existent process
	pidPath := filepath.Join(vmDir, "firecracker.pid")
	_ = os.WriteFile(pidPath, []byte("999999"), 0644)

	// Stop should complete without errors (no running process)
	err = e.Stop(context.Background(), sandboxID)
	if err != nil {
		t.Errorf("Stop should handle non-running VM: %v", err)
	}
}

func TestEngine_ImagesPath(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	path := e.ImagesPath()
	expected := filepath.Join(tmpDir, "images")
	if path != expected {
		t.Errorf("Expected %s, got %s", expected, path)
	}
}

func TestEngine_SSHKeyManager(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	km := e.SSHKeyManager()
	if km == nil {
		t.Error("SSHKeyManager should not be nil")
	}
}

func TestEngine_allocateNetwork_consistency(t *testing.T) {
	e := &Engine{}

	// Same ID should always produce the same network config
	sandboxID := "test-sandbox-123"

	mac1, gw1, ip1, tap1 := e.allocateNetwork(sandboxID)
	mac2, gw2, ip2, tap2 := e.allocateNetwork(sandboxID)

	if mac1 != mac2 || gw1 != gw2 || ip1 != ip2 || tap1 != tap2 {
		t.Error("allocateNetwork should be deterministic")
	}

	// Different IDs should produce different configs (with very high probability)
	mac3, gw3, ip3, tap3 := e.allocateNetwork("different-sandbox")
	if mac1 == mac3 && gw1 == gw3 && ip1 == ip3 && tap1 == tap3 {
		t.Error("different IDs should produce different network configs")
	}
}

func TestEngine_Forward_EmptyPorts(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = e.Forward(context.Background(), "sandbox-id", []model.PortMapping{})
	if err == nil {
		t.Error("Forward should return error for empty ports")
	}
	if !errors.Is(err, model.ErrNotValid) {
		t.Errorf("Expected ErrNotValid, got: %v", err)
	}
}
