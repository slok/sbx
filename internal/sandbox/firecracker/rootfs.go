package firecracker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// RootFSFile is the filename for the VM's rootfs copy.
	RootFSFile = "rootfs.ext4"
	// AuthorizedKeysPath is the path inside the rootfs for SSH authorized keys.
	AuthorizedKeysPath = "/root/.ssh/authorized_keys"
)

// copyRootFS copies the base rootfs to the VM directory.
func (e *Engine) copyRootFS(srcPath, vmDir string) error {
	dstPath := filepath.Join(vmDir, RootFSFile)

	// Open source file
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("could not open source rootfs: %w", err)
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("could not create destination rootfs: %w", err)
	}
	defer dst.Close()

	// Copy the file
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("could not copy rootfs: %w", err)
	}

	// Sync to disk
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("could not sync rootfs: %w", err)
	}

	e.logger.Debugf("Copied rootfs from %s to %s", srcPath, dstPath)
	return nil
}

// patchRootFSSSH patches the rootfs with the SSH public key.
// This uses debugfs (from e2fsprogs) to inject the key without mounting.
func (e *Engine) patchRootFSSSH(vmDir string) error {
	rootfsPath := filepath.Join(vmDir, RootFSFile)

	// Get the SSH public key
	pubKey, err := e.sshKeyManager.LoadPublicKey()
	if err != nil {
		return fmt.Errorf("could not load SSH public key: %w", err)
	}
	pubKey = strings.TrimSpace(pubKey)

	// Create a temporary file with the authorized_keys content
	tmpFile, err := os.CreateTemp("", "authorized_keys")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(pubKey + "\n"); err != nil {
		tmpFile.Close()
		return fmt.Errorf("could not write to temp file: %w", err)
	}
	tmpFile.Close()

	// Check if debugfs is available
	if _, err := exec.LookPath("debugfs"); err != nil {
		return fmt.Errorf("debugfs not found (install e2fsprogs): %w", err)
	}

	// Use debugfs to create .ssh directory and write authorized_keys
	// Commands:
	// 1. rm /root/.ssh/authorized_keys (remove existing file if present - base image may have one)
	// 2. mkdir /root/.ssh (if not exists - will fail silently if exists)
	// 3. Set .ssh directory permissions to 700 (directory type + rwx------)
	// 4. write <tmpfile> /root/.ssh/authorized_keys
	// 5. Set authorized_keys permissions to 600 (regular file type + rw-------)
	//
	// Note: debugfs set_inode_field uses octal values directly
	// mode for directory with 700: 040700
	// mode for file with 600: 0100600
	//
	// Important: rm must come before mkdir because mkdir errors if dir exists,
	// and that error stops subsequent set_inode_field from working properly.
	commands := fmt.Sprintf(`rm /root/.ssh/authorized_keys
mkdir /root/.ssh
set_inode_field /root/.ssh mode 040700
write %s /root/.ssh/authorized_keys
set_inode_field /root/.ssh/authorized_keys mode 0100600
`, tmpPath)

	cmd := exec.Command("debugfs", "-w", rootfsPath)
	cmd.Stdin = strings.NewReader(commands)
	output, err := cmd.CombinedOutput()
	outStr := string(output)

	// Log the output for debugging
	e.logger.Debugf("debugfs output: %s", outStr)

	// Check for actual errors (not warnings about existing dirs/files)
	// debugfs often returns non-zero for benign warnings
	if err != nil {
		// Check if write actually failed
		if strings.Contains(outStr, "write:") && strings.Contains(outStr, "error") {
			return fmt.Errorf("debugfs write failed: %w, output: %s", err, outStr)
		}
	}

	// Verify the write succeeded by checking for "already exists" message
	// which indicates the rm didn't work or write failed
	if strings.Contains(outStr, "Ext2 file already exists") {
		return fmt.Errorf("failed to write authorized_keys: file already exists after rm")
	}

	e.logger.Debugf("Patched rootfs with SSH key at %s", rootfsPath)
	return nil
}

// patchRootFSDNS patches the rootfs with DNS configuration.
// This uses debugfs to write /etc/resolv.conf so DNS works immediately on boot
// without needing post-boot SSH configuration.
func (e *Engine) patchRootFSDNS(vmDir string) error {
	rootfsPath := filepath.Join(vmDir, RootFSFile)

	// Verify rootfs exists
	if _, err := os.Stat(rootfsPath); err != nil {
		return fmt.Errorf("rootfs not found at %s: %w", rootfsPath, err)
	}

	// Create a temporary resolv.conf file
	tmpFile, err := os.CreateTemp("", "resolv.conf")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write DNS configuration
	resolvConf := "nameserver 1.1.1.1\nnameserver 8.8.8.8\n"
	if _, err := tmpFile.WriteString(resolvConf); err != nil {
		tmpFile.Close()
		return fmt.Errorf("could not write to temp file: %w", err)
	}
	tmpFile.Close()

	// Check if debugfs is available
	if _, err := exec.LookPath("debugfs"); err != nil {
		return fmt.Errorf("debugfs not found (install e2fsprogs): %w", err)
	}

	// Use debugfs to overwrite /etc/resolv.conf in the rootfs image.
	// 1. rm existing resolv.conf (may not exist, that's ok)
	// 2. write our resolv.conf
	// 3. set permissions to 644 (regular file: 0100644)
	commands := fmt.Sprintf(`rm /etc/resolv.conf
write %s /etc/resolv.conf
set_inode_field /etc/resolv.conf mode 0100644
`, tmpPath)

	cmd := exec.Command("debugfs", "-w", rootfsPath)
	cmd.Stdin = strings.NewReader(commands)
	output, err := cmd.CombinedOutput()
	outStr := string(output)

	e.logger.Debugf("debugfs DNS output: %s", outStr)

	if err != nil {
		if strings.Contains(outStr, "write:") && strings.Contains(outStr, "error") {
			return fmt.Errorf("debugfs write failed: %w, output: %s", err, outStr)
		}
	}

	e.logger.Debugf("Patched rootfs with DNS configuration at %s", rootfsPath)
	return nil
}

// sbxInitScript is the init wrapper script injected into the rootfs.
// It runs before the real init to set up things that the distro's init
// may not handle in a minimal/microVM environment (e.g., Alpine).
const sbxInitScript = `#!/bin/sh
# sbx-init: Pre-init setup for SBX Firecracker VMs.
# This script runs as PID 1 before handing off to the real init.

# Mount devpts for PTY support (needed for SSH TTY allocation).
# Without this, interactive shells (sbx shell) fail on minimal distros.
if ! mountpoint -q /dev/pts 2>/dev/null; then
    mkdir -p /dev/pts
    mount -t devpts devpts /dev/pts
fi

# Hand off to the real init system.
exec /sbin/init
`

// patchRootFSInit injects the sbx-init wrapper script into the rootfs.
// This script runs as PID 1 (via kernel init= parameter) and sets up
// things like devpts before handing off to the real /sbin/init.
func (e *Engine) patchRootFSInit(vmDir string) error {
	rootfsPath := filepath.Join(vmDir, RootFSFile)

	// Verify rootfs exists
	if _, err := os.Stat(rootfsPath); err != nil {
		return fmt.Errorf("rootfs not found at %s: %w", rootfsPath, err)
	}

	// Create a temporary file with the init script
	tmpFile, err := os.CreateTemp("", "sbx-init")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(sbxInitScript); err != nil {
		tmpFile.Close()
		return fmt.Errorf("could not write to temp file: %w", err)
	}
	tmpFile.Close()

	// Check if debugfs is available
	if _, err := exec.LookPath("debugfs"); err != nil {
		return fmt.Errorf("debugfs not found (install e2fsprogs): %w", err)
	}

	// Use debugfs to inject /sbin/sbx-init into the rootfs.
	// 1. rm existing sbx-init (may not exist, that's ok)
	// 2. write the init script
	// 3. set permissions to 755 (regular file + rwxr-xr-x: 0100755)
	commands := fmt.Sprintf(`rm /sbin/sbx-init
write %s /sbin/sbx-init
set_inode_field /sbin/sbx-init mode 0100755
`, tmpPath)

	cmd := exec.Command("debugfs", "-w", rootfsPath)
	cmd.Stdin = strings.NewReader(commands)
	output, err := cmd.CombinedOutput()
	outStr := string(output)

	e.logger.Debugf("debugfs init output: %s", outStr)

	if err != nil {
		if strings.Contains(outStr, "write:") && strings.Contains(outStr, "error") {
			return fmt.Errorf("debugfs write failed: %w, output: %s", err, outStr)
		}
	}

	e.logger.Debugf("Patched rootfs with sbx-init script at %s", rootfsPath)
	return nil
}

// RootFSPath returns the path to the VM's rootfs.
func (e *Engine) RootFSPath(vmDir string) string {
	return filepath.Join(vmDir, RootFSFile)
}

// MaxDiskGB is the maximum allowed disk size in GB.
const MaxDiskGB = 25

// resizeRootFS extends the rootfs file to the specified size in GB.
// This uses sparse file extension (fast, doesn't allocate actual disk space until written).
// The actual filesystem expansion happens inside the VM after boot via expandFilesystem.
func (e *Engine) resizeRootFS(vmDir string, sizeGB int, baseImagePath string) error {
	// Validate maximum size
	if sizeGB > MaxDiskGB {
		return fmt.Errorf("disk_gb (%d) exceeds maximum allowed (%d GB)", sizeGB, MaxDiskGB)
	}

	// Get base image size to ensure we don't shrink
	baseInfo, err := os.Stat(baseImagePath)
	if err != nil {
		return fmt.Errorf("could not stat base image: %w", err)
	}
	baseSize := baseInfo.Size()

	// Calculate target size in bytes
	targetSize := int64(sizeGB) * 1024 * 1024 * 1024

	// Validate target size is not smaller than base image
	if targetSize < baseSize {
		baseSizeGB := float64(baseSize) / (1024 * 1024 * 1024)
		return fmt.Errorf("disk_gb (%d GB) is smaller than base image size (%.2f GB)", sizeGB, baseSizeGB)
	}

	// Get rootfs path in VM directory
	rootfsPath := e.RootFSPath(vmDir)

	// Get current size of copied rootfs
	currentInfo, err := os.Stat(rootfsPath)
	if err != nil {
		return fmt.Errorf("could not stat rootfs: %w", err)
	}

	// Skip if already at target size
	if currentInfo.Size() == targetSize {
		e.logger.Debugf("Rootfs already at target size (%d GB)", sizeGB)
		return nil
	}

	// Extend the file using truncate (sparse file extension)
	if err := os.Truncate(rootfsPath, targetSize); err != nil {
		return fmt.Errorf("could not resize rootfs: %w", err)
	}

	e.logger.Debugf("Resized rootfs to %d GB at %s", sizeGB, rootfsPath)
	return nil
}

// expandFilesystem expands the ext4 filesystem inside the VM to fill the available space.
// This must be called after the VM boots and network is configured (SSH access required).
func (e *Engine) expandFilesystem(ctx context.Context, vmIP string) error {
	// Get SSH key path
	keyPath := e.sshKeyManager.PrivateKeyPath()

	// Run resize2fs via SSH
	// -o StrictHostKeyChecking=no: Don't prompt for host key verification
	// -o UserKnownHostsFile=/dev/null: Don't save host key
	// -o ConnectTimeout=10: Timeout for connection
	cmd := exec.CommandContext(ctx, "ssh",
		"-i", keyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		fmt.Sprintf("root@%s", vmIP),
		"resize2fs", "/dev/vda",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("resize2fs failed: %w, output: %s", err, string(output))
	}

	e.logger.Debugf("Expanded filesystem inside VM: %s", strings.TrimSpace(string(output)))
	return nil
}
