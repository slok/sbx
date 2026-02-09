package firecracker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/slok/sbx/internal/conventions"
	"github.com/slok/sbx/internal/ssh"
	fileutil "github.com/slok/sbx/internal/utils/file"
)

// copyRootFS copies the base rootfs to the VM directory.
func (e *Engine) copyRootFS(ctx context.Context, srcPath, vmDir string) error {
	dstPath := filepath.Join(vmDir, conventions.RootFSFile)

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

	copyErr := fileutil.CopyFileSparse(ctx, src, dst)
	if copyErr != nil {
		if errors.Is(copyErr, fileutil.ErrSparseUnsupported) {
			if _, err := src.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("could not seek source file before fallback copy: %w", err)
			}
			if err := dst.Truncate(0); err != nil {
				return fmt.Errorf("could not truncate destination before fallback copy: %w", err)
			}
			if _, err := dst.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("could not seek destination file before fallback copy: %w", err)
			}

			e.logger.Debugf("Sparse copy unsupported by filesystem/kernel while copying rootfs, using regular copy fallback")
			if _, err := io.Copy(dst, src); err != nil {
				return fmt.Errorf("could not copy rootfs: %w", err)
			}
		} else {
			return fmt.Errorf("could not copy rootfs: %w", copyErr)
		}
	}

	// Sync to disk
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("could not sync rootfs: %w", err)
	}

	e.logger.Debugf("Copied rootfs from %s to %s", srcPath, dstPath)
	return nil
}

// patchRootFSSSH patches the rootfs with the sandbox's SSH public key.
// This uses debugfs (from e2fsprogs) to inject the key without mounting.
func (e *Engine) patchRootFSSSH(sandboxID, vmDir string) error {
	rootfsPath := filepath.Join(vmDir, conventions.RootFSFile)

	// Get the per-sandbox SSH public key
	pubKey, err := e.sshKeyManager.LoadPublicKey(sandboxID)
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
	return filepath.Join(vmDir, conventions.RootFSFile)
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
// Retries with exponential backoff to wait for SSH to be available after boot.
func (e *Engine) expandFilesystem(ctx context.Context, sandboxID, vmIP string) error {
	// Retry logic: VM needs time to boot and start SSH service.
	maxRetries := 10
	baseDelay := 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 500ms, 1s, 2s, 4s, 8s...
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			if delay > 10*time.Second {
				delay = 10 * time.Second // Cap at 10s
			}
			e.logger.Debugf("Waiting %v before retry %d/%d", delay, attempt+1, maxRetries)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Connect via Go SSH client with short timeout for retries.
		client, err := e.newSSHClientWithTimeout(ctx, sandboxID, 5*time.Second)
		if err != nil {
			lastErr = fmt.Errorf("SSH not ready: %w", err)
			e.logger.Debugf("SSH connection failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}

		var stdout bytes.Buffer
		exitCode, err := client.Exec(ctx, "resize2fs /dev/vda", ssh.ExecOpts{
			Stdout: &stdout,
			Stderr: &stdout,
		})
		client.Close()

		if err != nil {
			lastErr = fmt.Errorf("SSH not ready: %w", err)
			e.logger.Debugf("SSH exec failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}

		if exitCode != 0 {
			return fmt.Errorf("resize2fs failed with exit code %d: %s", exitCode, stdout.String())
		}

		// Success!
		e.logger.Debugf("Expanded filesystem inside VM: %s", strings.TrimSpace(stdout.String()))
		return nil
	}

	// All retries exhausted.
	return fmt.Errorf("failed to expand filesystem after %d attempts: %w", maxRetries, lastErr)
}
