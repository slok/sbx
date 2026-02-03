package firecracker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
)

func TestEngine_resizeRootFS(t *testing.T) {
	tests := map[string]struct {
		baseImageSize int64 // bytes
		sizeGB        int
		expErr        bool
		expErrMsg     string
		expSize       int64 // expected final size in bytes
	}{
		"Resize to 10GB should extend file correctly": {
			baseImageSize: 300 * 1024 * 1024, // 300MB base image
			sizeGB:        10,
			expErr:        false,
			expSize:       10 * 1024 * 1024 * 1024, // 10GB
		},
		"Resize to 1GB with smaller base image should work": {
			baseImageSize: 500 * 1024 * 1024, // 500MB base image
			sizeGB:        1,
			expErr:        false,
			expSize:       1 * 1024 * 1024 * 1024, // 1GB
		},
		"Resize to maximum 25GB should work": {
			baseImageSize: 300 * 1024 * 1024, // 300MB base image
			sizeGB:        25,
			expErr:        false,
			expSize:       25 * 1024 * 1024 * 1024, // 25GB
		},
		"Resize exceeding 25GB limit should fail": {
			baseImageSize: 300 * 1024 * 1024, // 300MB base image
			sizeGB:        26,
			expErr:        true,
			expErrMsg:     "exceeds maximum allowed",
		},
		"Resize exceeding 100GB limit should fail": {
			baseImageSize: 300 * 1024 * 1024, // 300MB base image
			sizeGB:        100,
			expErr:        true,
			expErrMsg:     "exceeds maximum allowed",
		},
		"Resize smaller than base image should fail": {
			baseImageSize: 2 * 1024 * 1024 * 1024, // 2GB base image
			sizeGB:        1,                      // Try to resize to 1GB
			expErr:        true,
			expErrMsg:     "smaller than base image",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Create temp directory for test
			tmpDir, err := os.MkdirTemp("", "sbx-rootfs-test-*")
			require.NoError(err)
			defer os.RemoveAll(tmpDir)

			// Create a fake base image file with the specified size
			baseImagePath := filepath.Join(tmpDir, "base-image.ext4")
			err = createSparseFile(baseImagePath, tc.baseImageSize)
			require.NoError(err)

			// Create VM directory
			vmDir := filepath.Join(tmpDir, "vm")
			err = os.MkdirAll(vmDir, 0755)
			require.NoError(err)

			// Copy the "base image" to VM directory (simulating copyRootFS)
			rootfsPath := filepath.Join(vmDir, RootFSFile)
			err = copyFile(baseImagePath, rootfsPath)
			require.NoError(err)

			// Create engine
			e := &Engine{
				logger: log.Noop,
			}

			// Execute resize
			err = e.resizeRootFS(vmDir, tc.sizeGB, baseImagePath)

			if tc.expErr {
				assert.Error(err)
				if tc.expErrMsg != "" {
					assert.Contains(err.Error(), tc.expErrMsg)
				}
			} else {
				assert.NoError(err)

				// Verify the file was resized correctly
				info, err := os.Stat(rootfsPath)
				require.NoError(err)
				assert.Equal(tc.expSize, info.Size(), "rootfs should be resized to expected size")
			}
		})
	}
}

func TestEngine_resizeRootFS_sameSize(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "sbx-rootfs-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Create a base image that's already 1GB
	targetSize := int64(1 * 1024 * 1024 * 1024)
	baseImagePath := filepath.Join(tmpDir, "base-image.ext4")
	err = createSparseFile(baseImagePath, targetSize)
	require.NoError(err)

	// Create VM directory and copy base image
	vmDir := filepath.Join(tmpDir, "vm")
	err = os.MkdirAll(vmDir, 0755)
	require.NoError(err)

	rootfsPath := filepath.Join(vmDir, RootFSFile)
	err = copyFile(baseImagePath, rootfsPath)
	require.NoError(err)

	// Create engine
	e := &Engine{
		logger: log.Noop,
	}

	// Resize to same size (1GB) - should be a no-op
	err = e.resizeRootFS(vmDir, 1, baseImagePath)
	assert.NoError(err)

	// Verify size unchanged
	info, err := os.Stat(rootfsPath)
	require.NoError(err)
	assert.Equal(targetSize, info.Size())
}

func TestEngine_resizeRootFS_missingRootfs(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "sbx-rootfs-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Create base image
	baseImagePath := filepath.Join(tmpDir, "base-image.ext4")
	err = createSparseFile(baseImagePath, 300*1024*1024)
	require.NoError(err)

	// Create VM directory but DON'T copy rootfs
	vmDir := filepath.Join(tmpDir, "vm")
	err = os.MkdirAll(vmDir, 0755)
	require.NoError(err)

	// Create engine
	e := &Engine{
		logger: log.Noop,
	}

	// Try to resize non-existent rootfs
	err = e.resizeRootFS(vmDir, 10, baseImagePath)
	assert.Error(err)
	assert.Contains(err.Error(), "could not stat rootfs")
}

func TestEngine_resizeRootFS_missingBaseImage(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "sbx-rootfs-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Create VM directory with a rootfs file
	vmDir := filepath.Join(tmpDir, "vm")
	err = os.MkdirAll(vmDir, 0755)
	require.NoError(err)

	rootfsPath := filepath.Join(vmDir, RootFSFile)
	err = createSparseFile(rootfsPath, 300*1024*1024)
	require.NoError(err)

	// Create engine
	e := &Engine{
		logger: log.Noop,
	}

	// Try to resize with non-existent base image
	err = e.resizeRootFS(vmDir, 10, "/nonexistent/base.ext4")
	assert.Error(err)
	assert.Contains(err.Error(), "could not stat base image")
}

func TestEngine_patchRootFSDNS(t *testing.T) {
	// Skip if debugfs is not available
	if _, err := exec.LookPath("debugfs"); err != nil {
		t.Skip("debugfs not available, skipping test")
	}
	// Skip if mkfs.ext4 is not available
	if _, err := exec.LookPath("mkfs.ext4"); err != nil {
		t.Skip("mkfs.ext4 not available, skipping test")
	}

	tests := map[string]struct {
		setupRootfs bool // whether to create a valid ext4 rootfs
		expErr      bool
		expErrMsg   string
	}{
		"Should patch DNS in valid ext4 rootfs": {
			setupRootfs: true,
			expErr:      false,
		},
		"Should fail when rootfs file does not exist": {
			setupRootfs: false,
			expErr:      true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			tmpDir, err := os.MkdirTemp("", "sbx-dns-test-*")
			require.NoError(err)
			defer os.RemoveAll(tmpDir)

			vmDir := filepath.Join(tmpDir, "vm")
			err = os.MkdirAll(vmDir, 0755)
			require.NoError(err)

			if tc.setupRootfs {
				// Create a small real ext4 image with /etc directory
				rootfsPath := filepath.Join(vmDir, RootFSFile)
				err = createExt4Image(rootfsPath, "10M")
				require.NoError(err)

				// Create /etc directory in the image
				cmd := exec.Command("debugfs", "-w", rootfsPath)
				cmd.Stdin = strings.NewReader("mkdir /etc\n")
				_ = cmd.Run()
			}

			e := &Engine{
				logger: log.Noop,
			}

			err = e.patchRootFSDNS(vmDir)

			if tc.expErr {
				assert.Error(err)
				if tc.expErrMsg != "" {
					assert.Contains(err.Error(), tc.expErrMsg)
				}
			} else {
				assert.NoError(err)

				// Verify the DNS config was written by reading it back with debugfs
				rootfsPath := filepath.Join(vmDir, RootFSFile)
				cmd := exec.Command("debugfs", "-R", "cat /etc/resolv.conf", rootfsPath)
				output, err := cmd.Output()
				require.NoError(err)
				content := string(output)
				assert.Contains(content, "nameserver 1.1.1.1")
				assert.Contains(content, "nameserver 8.8.8.8")
			}
		})
	}
}

func TestEngine_patchRootFSDNS_overwritesExisting(t *testing.T) {
	// Skip if debugfs or mkfs.ext4 is not available
	if _, err := exec.LookPath("debugfs"); err != nil {
		t.Skip("debugfs not available, skipping test")
	}
	if _, err := exec.LookPath("mkfs.ext4"); err != nil {
		t.Skip("mkfs.ext4 not available, skipping test")
	}

	assert := assert.New(t)
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "sbx-dns-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	vmDir := filepath.Join(tmpDir, "vm")
	err = os.MkdirAll(vmDir, 0755)
	require.NoError(err)

	rootfsPath := filepath.Join(vmDir, RootFSFile)
	err = createExt4Image(rootfsPath, "10M")
	require.NoError(err)

	// Create /etc and write an existing resolv.conf
	oldResolvConf, err := os.CreateTemp("", "old-resolv-*")
	require.NoError(err)
	defer os.Remove(oldResolvConf.Name())
	_, err = oldResolvConf.WriteString("nameserver 192.168.1.1\n")
	require.NoError(err)
	oldResolvConf.Close()

	cmd := exec.Command("debugfs", "-w", rootfsPath)
	cmd.Stdin = strings.NewReader(fmt.Sprintf("mkdir /etc\nwrite %s /etc/resolv.conf\n", oldResolvConf.Name()))
	_ = cmd.Run()

	e := &Engine{
		logger: log.Noop,
	}

	// Patch should overwrite existing resolv.conf
	err = e.patchRootFSDNS(vmDir)
	assert.NoError(err)

	// Verify it was overwritten
	cmd = exec.Command("debugfs", "-R", "cat /etc/resolv.conf", rootfsPath)
	output, err := cmd.Output()
	require.NoError(err)
	content := string(output)
	assert.Contains(content, "nameserver 1.1.1.1")
	assert.Contains(content, "nameserver 8.8.8.8")
	assert.NotContains(content, "192.168.1.1")
}

// createExt4Image creates a small ext4 image of the given size (e.g. "10M").
func createExt4Image(path, size string) error {
	// Create sparse file
	cmd := exec.Command("truncate", "-s", size, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("truncate failed: %w, output: %s", err, string(output))
	}
	// Format as ext4
	cmd = exec.Command("mkfs.ext4", "-F", "-q", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs.ext4 failed: %w, output: %s", err, string(output))
	}
	return nil
}

// Helper functions

// createSparseFile creates a sparse file of the given size.
func createSparseFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := f.Truncate(size); err != nil {
		return err
	}
	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		// For sparse files, we can't just read all bytes
		// Instead, create dst with same size
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		return createSparseFile(dst, info.Size())
	}
	return os.WriteFile(dst, data, 0644)
}
