package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyManager_GenerateKeys(t *testing.T) {
	tmpDir := t.TempDir()
	keyDir := filepath.Join(tmpDir, "ssh")

	km := NewKeyManager(keyDir)

	// Initially keys should not exist
	if km.KeysExist() {
		t.Error("keys should not exist initially")
	}

	// Generate keys
	pubKey, err := km.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys() failed: %v", err)
	}

	// Verify public key format
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Errorf("public key should start with 'ssh-ed25519 ', got: %s", pubKey[:min(30, len(pubKey))])
	}

	// Keys should now exist
	if !km.KeysExist() {
		t.Error("keys should exist after generation")
	}

	// Verify files exist with correct permissions
	privInfo, err := os.Stat(km.PrivateKeyPath())
	if err != nil {
		t.Fatalf("private key file not found: %v", err)
	}
	if privInfo.Mode().Perm() != 0600 {
		t.Errorf("private key should have 0600 permissions, got %o", privInfo.Mode().Perm())
	}

	pubInfo, err := os.Stat(km.PublicKeyPath())
	if err != nil {
		t.Fatalf("public key file not found: %v", err)
	}
	if pubInfo.Mode().Perm() != 0644 {
		t.Errorf("public key should have 0644 permissions, got %o", pubInfo.Mode().Perm())
	}
}

func TestKeyManager_EnsureKeys(t *testing.T) {
	tmpDir := t.TempDir()
	keyDir := filepath.Join(tmpDir, "ssh")

	km := NewKeyManager(keyDir)

	// First call should generate keys
	pubKey1, err := km.EnsureKeys()
	if err != nil {
		t.Fatalf("EnsureKeys() failed: %v", err)
	}

	// Second call should return existing keys (same content)
	pubKey2, err := km.EnsureKeys()
	if err != nil {
		t.Fatalf("EnsureKeys() second call failed: %v", err)
	}

	if pubKey1 != pubKey2 {
		t.Error("EnsureKeys() should return same key on subsequent calls")
	}
}

func TestKeyManager_LoadPrivateKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyDir := filepath.Join(tmpDir, "ssh")

	km := NewKeyManager(keyDir)

	// Generate keys first
	_, err := km.GenerateKeys()
	if err != nil {
		t.Fatalf("GenerateKeys() failed: %v", err)
	}

	// Load private key
	privKey, err := km.LoadPrivateKey()
	if err != nil {
		t.Fatalf("LoadPrivateKey() failed: %v", err)
	}

	// Verify it's a valid PEM
	if !strings.Contains(string(privKey), "BEGIN OPENSSH PRIVATE KEY") {
		t.Error("private key should be in OpenSSH PEM format")
	}
}

func TestKeyManager_KeyPaths(t *testing.T) {
	km := NewKeyManager("/home/user/.sbx/ssh")

	if km.PrivateKeyPath() != "/home/user/.sbx/ssh/id_ed25519" {
		t.Errorf("unexpected private key path: %s", km.PrivateKeyPath())
	}

	if km.PublicKeyPath() != "/home/user/.sbx/ssh/id_ed25519.pub" {
		t.Errorf("unexpected public key path: %s", km.PublicKeyPath())
	}
}
