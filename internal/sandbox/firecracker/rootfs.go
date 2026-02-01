package firecracker

import (
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

// RootFSPath returns the path to the VM's rootfs.
func (e *Engine) RootFSPath(vmDir string) string {
	return filepath.Join(vmDir, RootFSFile)
}
