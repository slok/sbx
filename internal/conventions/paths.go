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

	// SSH key files.

	// SSHPrivateKeyFile is the filename for the per-sandbox SSH private key.
	SSHPrivateKeyFile = "id_ed25519"
	// SSHPublicKeyFile is the filename for the per-sandbox SSH public key.
	SSHPublicKeyFile = "id_ed25519.pub"
	// AuthorizedKeysPath is the path inside the rootfs for SSH authorized keys.
	AuthorizedKeysPath = "/root/.ssh/authorized_keys"

	// Egress proxy files.

	// EgressProxyPIDFile is the PID filename for the egress proxy process.
	EgressProxyPIDFile = "egress-proxy.pid"
	// EgressProxyLogFile is the log filename for the egress proxy.
	EgressProxyLogFile = "egress-proxy.log"
	// EgressProxyPort is the default port the egress TCP proxy listens on.
	EgressProxyPort = 8443
	// EgressDNSPort is the port the egress DNS forwarder listens on.
	// Uses a non-privileged port (>1024) so the process doesn't need
	// CAP_NET_BIND_SERVICE. Nftables DNAT redirects UDP 53 to this port.
	EgressDNSPort = 5353
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
