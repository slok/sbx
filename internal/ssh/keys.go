package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/slok/sbx/internal/conventions"
)

// KeyManager handles per-sandbox SSH key generation and loading.
// It uses conventions to derive key paths from dataDir + sandboxID.
type KeyManager struct {
	dataDir string
}

// NewKeyManager creates a new SSH key manager.
// dataDir is the sbx data directory (e.g., ~/.sbx).
func NewKeyManager(dataDir string) *KeyManager {
	return &KeyManager{dataDir: dataDir}
}

// PrivateKeyPath returns the path to a sandbox's private key.
func (m *KeyManager) PrivateKeyPath(sandboxID string) string {
	return conventions.SSHPrivateKeyPath(m.dataDir, sandboxID)
}

// PublicKeyPath returns the path to a sandbox's public key.
func (m *KeyManager) PublicKeyPath(sandboxID string) string {
	return conventions.SSHPublicKeyPath(m.dataDir, sandboxID)
}

// KeysExist checks if both private and public keys exist for a sandbox.
func (m *KeyManager) KeysExist(sandboxID string) bool {
	_, errPriv := os.Stat(m.PrivateKeyPath(sandboxID))
	_, errPub := os.Stat(m.PublicKeyPath(sandboxID))
	return errPriv == nil && errPub == nil
}

// GenerateKeys generates a new Ed25519 SSH key pair for a sandbox.
// The key directory (VM dir) must already exist.
// Returns the public key in authorized_keys format.
func (m *KeyManager) GenerateKeys(sandboxID string) (publicKeyAuthorized string, err error) {
	keyDir := conventions.VMDir(m.dataDir, sandboxID)

	// Ensure directory exists (should already exist from VM creation, but be safe).
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return "", fmt.Errorf("could not create ssh key directory: %w", err)
	}

	// Generate Ed25519 key pair.
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("could not generate ed25519 key: %w", err)
	}

	// Convert to SSH format.
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("could not convert to ssh public key: %w", err)
	}

	// Marshal private key to OpenSSH format.
	privKeyBytes, err := ssh.MarshalPrivateKey(privKey, "sbx-generated-key")
	if err != nil {
		return "", fmt.Errorf("could not marshal private key: %w", err)
	}

	// Write private key.
	privKeyPath := m.PrivateKeyPath(sandboxID)
	if err := os.WriteFile(privKeyPath, pem.EncodeToMemory(privKeyBytes), 0600); err != nil {
		return "", fmt.Errorf("could not write private key: %w", err)
	}

	// Write public key in authorized_keys format.
	publicKeyAuthorized = string(ssh.MarshalAuthorizedKey(sshPubKey))
	pubKeyPath := m.PublicKeyPath(sandboxID)
	if err := os.WriteFile(pubKeyPath, []byte(publicKeyAuthorized), 0644); err != nil {
		os.Remove(privKeyPath)
		return "", fmt.Errorf("could not write public key: %w", err)
	}

	return publicKeyAuthorized, nil
}

// LoadPublicKey reads a sandbox's public key in authorized_keys format.
func (m *KeyManager) LoadPublicKey(sandboxID string) (string, error) {
	data, err := os.ReadFile(m.PublicKeyPath(sandboxID))
	if err != nil {
		return "", fmt.Errorf("could not read public key: %w", err)
	}
	return string(data), nil
}

// LoadPrivateKey reads a sandbox's private key bytes.
func (m *KeyManager) LoadPrivateKey(sandboxID string) ([]byte, error) {
	data, err := os.ReadFile(m.PrivateKeyPath(sandboxID))
	if err != nil {
		return nil, fmt.Errorf("could not read private key: %w", err)
	}
	return data, nil
}
