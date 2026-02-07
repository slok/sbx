package firecracker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

func TestEngineCreateSnapshot(t *testing.T) {
	tests := map[string]struct {
		prepare func(t *testing.T, e *Engine, sandboxID string, dstPath string)
		expErr  bool
		errIs   error
	}{
		"Creating snapshot from existing rootfs should work": {
			prepare: func(t *testing.T, e *Engine, sandboxID string, _ string) {
				t.Helper()

				vmDir := e.VMDir(sandboxID)
				require.NoError(t, os.MkdirAll(vmDir, 0755))
				require.NoError(t, createSparseRootFS(filepath.Join(vmDir, RootFSFile), 64*1024*1024))
			},
		},
		"Creating snapshot when destination exists should fail": {
			prepare: func(t *testing.T, e *Engine, sandboxID string, dstPath string) {
				t.Helper()

				vmDir := e.VMDir(sandboxID)
				require.NoError(t, os.MkdirAll(vmDir, 0755))
				require.NoError(t, createSparseRootFS(filepath.Join(vmDir, RootFSFile), 8*1024*1024))

				require.NoError(t, os.MkdirAll(filepath.Dir(dstPath), 0755))
				f, err := os.Create(dstPath)
				require.NoError(t, err)
				require.NoError(t, f.Close())
			},
			expErr: true,
			errIs:  model.ErrAlreadyExists,
		},
		"Creating snapshot when source rootfs is missing should fail": {
			expErr: true,
			errIs:  model.ErrNotFound,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			dataDir := t.TempDir()
			e, err := NewEngine(EngineConfig{DataDir: dataDir, Logger: log.Noop})
			require.NoError(err)

			sandboxID := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
			snapshotID := "01ARZ3NDEKTSV4RRFFQ69G5FAB"
			dstPath := filepath.Join(e.SnapshotsPath(), snapshotID+".ext4")

			if test.prepare != nil {
				test.prepare(t, e, sandboxID, dstPath)
			}

			virtualSize, allocatedSize, err := e.CreateSnapshot(context.Background(), sandboxID, snapshotID, dstPath)

			if test.expErr {
				assert.Error(err)
				if test.errIs != nil {
					assert.True(errors.Is(err, test.errIs))
				}
				return
			}

			require.NoError(err)
			assert.FileExists(dstPath)
			assert.GreaterOrEqual(virtualSize, int64(1))
			assert.GreaterOrEqual(allocatedSize, int64(0))
			assert.LessOrEqual(allocatedSize, virtualSize)
		})
	}
}

func createSparseRootFS(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := f.Truncate(size); err != nil {
		return err
	}

	if _, err := f.WriteAt([]byte("A"), 0); err != nil {
		return err
	}

	if size > 1024 {
		if _, err := f.WriteAt([]byte("Z"), size-1024); err != nil {
			return err
		}
	}

	return f.Sync()
}
