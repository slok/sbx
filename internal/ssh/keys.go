package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

const (
	// DefaultKeyDir is the default directory for SSH keys relative to sbx data dir.
	DefaultKeyDir = "ssh"
	// PrivateKeyFile is the filename for the private key.
	PrivateKeyFile = "id_ed25519"
	// PublicKeyFile is the filename for the public key.
	PublicKeyFile = "id_ed25519.pub"
)

// KeyManager handles SSH key generation and loading.
type KeyManager struct {
	keyDir string
}

// NewKeyManager creates a new SSH key manager.
// keyDir should be the full path to the SSH key directory (e.g., ~/.sbx/ssh).
func NewKeyManager(keyDir string) *KeyManager {
	return &KeyManager{keyDir: keyDir}
}

// PrivateKeyPath returns the path to the private key.
func (m *KeyManager) PrivateKeyPath() string {
	return filepath.Join(m.keyDir, PrivateKeyFile)
}

// PublicKeyPath returns the path to the public key.
func (m *KeyManager) PublicKeyPath() string {
	return filepath.Join(m.keyDir, PublicKeyFile)
}

// KeysExist checks if both private and public keys exist.
func (m *KeyManager) KeysExist() bool {
	_, errPriv := os.Stat(m.PrivateKeyPath())
	_, errPub := os.Stat(m.PublicKeyPath())
	return errPriv == nil && errPub == nil
}

// EnsureKeys generates SSH keys if they don't exist, or returns existing keys.
// Returns the public key in authorized_keys format.
func (m *KeyManager) EnsureKeys() (publicKeyAuthorized string, err error) {
	if m.KeysExist() {
		return m.LoadPublicKey()
	}
	return m.GenerateKeys()
}

// GenerateKeys generates a new Ed25519 SSH key pair.
// Returns the public key in authorized_keys format.
func (m *KeyManager) GenerateKeys() (publicKeyAuthorized string, err error) {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(m.keyDir, 0700); err != nil {
		return "", fmt.Errorf("could not create ssh key directory: %w", err)
	}

	// Generate Ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("could not generate ed25519 key: %w", err)
	}

	// Convert to SSH format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("could not convert to ssh public key: %w", err)
	}

	// Marshal private key to OpenSSH format
	privKeyBytes, err := ssh.MarshalPrivateKey(privKey, "sbx-generated-key")
	if err != nil {
		return "", fmt.Errorf("could not marshal private key: %w", err)
	}

	// Write private key
	privKeyPath := m.PrivateKeyPath()
	if err := os.WriteFile(privKeyPath, pem.EncodeToMemory(privKeyBytes), 0600); err != nil {
		return "", fmt.Errorf("could not write private key: %w", err)
	}

	// Write public key in authorized_keys format
	pubKeyAuthorized := string(ssh.MarshalAuthorizedKey(sshPubKey))
	pubKeyPath := m.PublicKeyPath()
	if err := os.WriteFile(pubKeyPath, []byte(pubKeyAuthorized), 0644); err != nil {
		// Clean up private key on error
		os.Remove(privKeyPath)
		return "", fmt.Errorf("could not write public key: %w", err)
	}

	return pubKeyAuthorized, nil
}

// LoadPublicKey reads the public key in authorized_keys format.
func (m *KeyManager) LoadPublicKey() (string, error) {
	data, err := os.ReadFile(m.PublicKeyPath())
	if err != nil {
		return "", fmt.Errorf("could not read public key: %w", err)
	}
	return string(data), nil
}

// LoadPrivateKey reads the private key bytes.
func (m *KeyManager) LoadPrivateKey() ([]byte, error) {
	data, err := os.ReadFile(m.PrivateKeyPath())
	if err != nil {
		return nil, fmt.Errorf("could not read private key: %w", err)
	}
	return data, nil
}
