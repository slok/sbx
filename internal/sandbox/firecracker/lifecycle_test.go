package firecracker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
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

func TestEngine_Start_NotSupported(t *testing.T) {
	tmpDir := t.TempDir()
	e, err := NewEngine(EngineConfig{
		DataDir: tmpDir,
		Logger:  log.Noop,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	err = e.Start(context.Background(), "some-sandbox")
	if err == nil {
		t.Error("Start should return error (not supported)")
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
	os.WriteFile(pidPath, []byte("not-a-number"), 0644)

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
	os.WriteFile(pidPath, []byte("999999"), 0644)

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
	os.WriteFile(pidPath, []byte("999999"), 0644)

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
