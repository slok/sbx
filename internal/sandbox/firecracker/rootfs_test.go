package firecracker

import (
	"os"
	"path/filepath"
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
		"Resize to 1GB with smaller base image should work": {
			baseImageSize: 500 * 1024 * 1024, // 500MB base image
			sizeGB:        1,
			expErr:        false,
			expSize:       1 * 1024 * 1024 * 1024, // 1GB
		},
		"Resize smaller than base image should fail": {
			baseImageSize: 1100 * 1024 * 1024, // 1.1GB base image (just over 1GB target)
			sizeGB:        1,                  // Try to resize to 1GB
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

func TestEngine_patchRootFSDNS_missingRootfs(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	tmpDir, err := os.MkdirTemp("", "sbx-dns-test-*")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	vmDir := filepath.Join(tmpDir, "vm")
	err = os.MkdirAll(vmDir, 0755)
	require.NoError(err)

	e := &Engine{
		logger: log.Noop,
	}

	// Should fail when rootfs doesn't exist
	err = e.patchRootFSDNS(vmDir)
	assert.Error(err)
	assert.Contains(err.Error(), "rootfs not found")
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

// copyFile copies a file from src to dst using sparse file creation.
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return createSparseFile(dst, info.Size())
}
