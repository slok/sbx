package snapshotremove_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/snapshotremove"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config snapshotremove.ServiceConfig
		expErr bool
	}{
		"valid config": {
			config: snapshotremove.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
		"missing repository": {
			config: snapshotremove.ServiceConfig{},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			svc, err := snapshotremove.NewService(test.config)
			if test.expErr {
				require.Error(err)
			} else {
				require.NoError(err)
				require.NotNil(svc)
			}
		})
	}
}

func TestServiceRun(t *testing.T) {
	baseSnapshot := &model.Snapshot{
		ID:                 "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Name:               "my-snapshot",
		SourceSandboxID:    "01ARZ3NDEKTSV4RRFFQ69G5FBB",
		SourceSandboxName:  "my-sandbox",
		VirtualSizeBytes:   1024,
		AllocatedSizeBytes: 512,
		CreatedAt:          time.Date(2026, 2, 7, 10, 0, 0, 0, time.UTC),
	}

	tests := map[string]struct {
		req    snapshotremove.Request
		setup  func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot
		expErr bool
		errIs  error
	}{
		"Removing an existing snapshot by name should work": {
			req: snapshotremove.Request{NameOrID: "my-snapshot"},
			setup: func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot {
				t.Helper()
				tmpFile := filepath.Join(t.TempDir(), "snap.ext4")
				require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0644))

				snap := *baseSnapshot
				snap.Path = tmpFile
				repo.On("GetSnapshotByName", mock.Anything, "my-snapshot").Return(&snap, nil)
				repo.On("DeleteSnapshot", mock.Anything, snap.ID).Return(nil)
				return &snap
			},
		},
		"Removing a snapshot by ULID ID should work": {
			req: snapshotremove.Request{NameOrID: "01ARZ3NDEKTSV4RRFFQ69G5FAA"},
			setup: func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot {
				t.Helper()
				tmpFile := filepath.Join(t.TempDir(), "snap.ext4")
				require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0644))

				snap := *baseSnapshot
				snap.Path = tmpFile
				repo.On("GetSnapshotByName", mock.Anything, "01ARZ3NDEKTSV4RRFFQ69G5FAA").Return((*model.Snapshot)(nil), model.ErrNotFound)
				repo.On("GetSnapshot", mock.Anything, "01ARZ3NDEKTSV4RRFFQ69G5FAA").Return(&snap, nil)
				repo.On("DeleteSnapshot", mock.Anything, snap.ID).Return(nil)
				return &snap
			},
		},
		"Removing a snapshot when file is already gone should still succeed": {
			req: snapshotremove.Request{NameOrID: "my-snapshot"},
			setup: func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot {
				t.Helper()
				snap := *baseSnapshot
				snap.Path = filepath.Join(t.TempDir(), "nonexistent.ext4")
				repo.On("GetSnapshotByName", mock.Anything, "my-snapshot").Return(&snap, nil)
				repo.On("DeleteSnapshot", mock.Anything, snap.ID).Return(nil)
				return &snap
			},
		},
		"Removing a snapshot that is not found should fail": {
			req: snapshotremove.Request{NameOrID: "nonexistent"},
			setup: func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot {
				t.Helper()
				repo.On("GetSnapshotByName", mock.Anything, "nonexistent").Return((*model.Snapshot)(nil), model.ErrNotFound)
				return nil
			},
			expErr: true,
			errIs:  model.ErrNotFound,
		},
		"Repository delete error should propagate": {
			req: snapshotremove.Request{NameOrID: "my-snapshot"},
			setup: func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot {
				t.Helper()
				tmpFile := filepath.Join(t.TempDir(), "snap.ext4")
				require.NoError(t, os.WriteFile(tmpFile, []byte("data"), 0644))

				snap := *baseSnapshot
				snap.Path = tmpFile
				repo.On("GetSnapshotByName", mock.Anything, "my-snapshot").Return(&snap, nil)
				repo.On("DeleteSnapshot", mock.Anything, snap.ID).Return(errors.New("db error"))
				return nil
			},
			expErr: true,
		},
		"Repository get error should propagate": {
			req: snapshotremove.Request{NameOrID: "my-snapshot"},
			setup: func(t *testing.T, repo *storagemock.MockRepository) *model.Snapshot {
				t.Helper()
				repo.On("GetSnapshotByName", mock.Anything, "my-snapshot").Return((*model.Snapshot)(nil), errors.New("db error"))
				return nil
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			repo := storagemock.NewMockRepository(t)

			var expSnap *model.Snapshot
			if test.setup != nil {
				expSnap = test.setup(t, repo)
			}

			svc, err := snapshotremove.NewService(snapshotremove.ServiceConfig{
				Repository: repo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			result, err := svc.Run(context.Background(), test.req)

			if test.expErr {
				assert.Error(err)
				assert.Nil(result)
				if test.errIs != nil {
					assert.True(errors.Is(err, test.errIs))
				}
				return
			}

			require.NoError(err)
			require.NotNil(result)
			assert.Equal(expSnap.ID, result.ID)
			assert.Equal(expSnap.Name, result.Name)
		})
	}
}
