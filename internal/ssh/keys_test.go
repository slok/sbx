package ssh

import (
	"os"
	"strings"
	"testing"

	"github.com/slok/sbx/internal/conventions"
)

func TestKeyManager_GenerateKeys(t *testing.T) {
	dataDir := t.TempDir()
	sandboxID := "test-sandbox"

	// Create the VM directory (normally done by engine.Create).
	vmDir := conventions.VMDir(dataDir, sandboxID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatalf("failed to create vm dir: %v", err)
	}

	km := NewKeyManager(dataDir)

	// Initially keys should not exist.
	if km.KeysExist(sandboxID) {
		t.Error("keys should not exist initially")
	}

	// Generate keys.
	pubKey, err := km.GenerateKeys(sandboxID)
	if err != nil {
		t.Fatalf("GenerateKeys() failed: %v", err)
	}

	// Verify public key format.
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("public key should start with 'ssh-ed25519 ', got: %s", pubKey[:min(30, len(pubKey))])
	}

	// Keys should now exist.
	if !km.KeysExist(sandboxID) {
		t.Error("keys should exist after generation")
	}

	// Verify files exist with correct permissions.
	privInfo, err := os.Stat(km.PrivateKeyPath(sandboxID))
	if err != nil {
		t.Fatalf("private key file not found: %v", err)
	}
	if privInfo.Mode().Perm() != 0600 {
		t.Errorf("private key should have 0600 permissions, got %o", privInfo.Mode().Perm())
	}

	pubInfo, err := os.Stat(km.PublicKeyPath(sandboxID))
	if err != nil {
		t.Fatalf("public key file not found: %v", err)
	}
	if pubInfo.Mode().Perm() != 0644 {
		t.Errorf("public key should have 0644 permissions, got %o", pubInfo.Mode().Perm())
	}
}

func TestKeyManager_LoadPrivateKey(t *testing.T) {
	dataDir := t.TempDir()
	sandboxID := "test-sandbox"

	vmDir := conventions.VMDir(dataDir, sandboxID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		t.Fatalf("failed to create vm dir: %v", err)
	}

	km := NewKeyManager(dataDir)

	// Generate keys first.
	_, err := km.GenerateKeys(sandboxID)
	if err != nil {
		t.Fatalf("GenerateKeys() failed: %v", err)
	}

	// Load private key.
	privKey, err := km.LoadPrivateKey(sandboxID)
	if err != nil {
		t.Fatalf("LoadPrivateKey() failed: %v", err)
	}

	// Verify it's a valid PEM.
	if !strings.Contains(string(privKey), "BEGIN OPENSSH PRIVATE KEY") {
		t.Error("private key should be in OpenSSH PEM format")
	}
}

func TestKeyManager_KeyPaths(t *testing.T) {
	km := NewKeyManager("/home/user/.sbx")

	expectedPriv := conventions.SSHPrivateKeyPath("/home/user/.sbx", "my-sandbox")
	expectedPub := conventions.SSHPublicKeyPath("/home/user/.sbx", "my-sandbox")

	if km.PrivateKeyPath("my-sandbox") != expectedPriv {
		t.Errorf("unexpected private key path: %s", km.PrivateKeyPath("my-sandbox"))
	}

	if km.PublicKeyPath("my-sandbox") != expectedPub {
		t.Errorf("unexpected public key path: %s", km.PublicKeyPath("my-sandbox"))
	}
}

func TestKeyManager_DifferentSandboxes(t *testing.T) {
	dataDir := t.TempDir()
	km := NewKeyManager(dataDir)

	// Different sandboxes should have different key paths.
	path1 := km.PrivateKeyPath("sandbox-a")
	path2 := km.PrivateKeyPath("sandbox-b")

	if path1 == path2 {
		t.Error("different sandboxes should have different key paths")
	}
}
