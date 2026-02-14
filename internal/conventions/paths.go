package conventions

import "path/filepath"

const (
	// DefaultDataDir is the default sbx data directory name (relative to home).
	DefaultDataDir = ".sbx"
	// VMsDir is the subdirectory for VM data.
	VMsDir = "vms"
	// ImagesDir is the subdirectory for images.
	ImagesDir = "images"

	// VM-level files.

	// RootFSFile is the filename for the VM's rootfs copy.
	RootFSFile = "rootfs.ext4"
	// SocketFile is the Firecracker API socket filename.
	SocketFile = "firecracker.sock"
	// PIDFile is the Firecracker PID filename.
	PIDFile = "firecracker.pid"
	// LogFile is the Firecracker log filename.
	LogFile = "firecracker.log"

	// Proxy files.

	// ProxyPIDFile is the proxy process PID filename.
	ProxyPIDFile = "proxy.pid"
	// ProxyLogFile is the proxy log filename.
	ProxyLogFile = "proxy.log"
	// ProxyPortFile is the JSON file storing allocated proxy ports.
	ProxyPortFile = "proxy.json"

	// SSH key files.

	// SSHPrivateKeyFile is the filename for the per-sandbox SSH private key.
	SSHPrivateKeyFile = "id_ed25519"
	// SSHPublicKeyFile is the filename for the per-sandbox SSH public key.
	SSHPublicKeyFile = "id_ed25519.pub"
	// AuthorizedKeysPath is the path inside the rootfs for SSH authorized keys.
	AuthorizedKeysPath = "/root/.ssh/authorized_keys"
)

// VMDir returns the directory for a specific sandbox VM.
func VMDir(dataDir, sandboxID string) string {
	return filepath.Join(dataDir, VMsDir, sandboxID)
}

// VMFilePath returns the full path to a file inside a sandbox VM directory.
func VMFilePath(dataDir, sandboxID, filename string) string {
	return filepath.Join(VMDir(dataDir, sandboxID), filename)
}

// SSHPrivateKeyPath returns the path to a sandbox's SSH private key.
func SSHPrivateKeyPath(dataDir, sandboxID string) string {
	return VMFilePath(dataDir, sandboxID, SSHPrivateKeyFile)
}

// SSHPublicKeyPath returns the path to a sandbox's SSH public key.
func SSHPublicKeyPath(dataDir, sandboxID string) string {
	return VMFilePath(dataDir, sandboxID, SSHPublicKeyFile)
}
