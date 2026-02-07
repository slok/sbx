package snapshotcreate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/snapshotcreate"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestServiceRun(t *testing.T) {
	baseSandbox := &model.Sandbox{
		ID:     "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Name:   "sandbox1",
		Status: model.SandboxStatusStopped,
		Config: model.SandboxConfig{
			Name: "sandbox1",
			FirecrackerEngine: &model.FirecrackerEngineConfig{
				RootFS:      "/images/rootfs.ext4",
				KernelImage: "/images/vmlinux",
			},
			Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
		},
	}

	tests := map[string]struct {
		req    snapshotcreate.Request
		setup  func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine)
		expErr bool
		errIs  error
	}{
		"Creating snapshot with valid request should work": {
			req: snapshotcreate.Request{NameOrID: baseSandbox.Name, SnapshotName: "snap-1"},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				repo.On("GetSandboxByName", mock.Anything, baseSandbox.Name).Return(baseSandbox, nil)
				repo.On("GetSnapshotByName", mock.Anything, "snap-1").Return((*model.Snapshot)(nil), model.ErrNotFound)
				eng.On("CreateSnapshot", mock.Anything, baseSandbox.ID, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(int64(1024), int64(512), nil)
				repo.On("CreateSnapshot", mock.Anything, mock.MatchedBy(func(snapshot model.Snapshot) bool {
					return snapshot.Name == "snap-1" && snapshot.SourceSandboxID == baseSandbox.ID
				})).Return(nil)
			},
		},
		"Creating snapshot without name should auto-generate one": {
			req: snapshotcreate.Request{NameOrID: baseSandbox.Name},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				repo.On("GetSandboxByName", mock.Anything, baseSandbox.Name).Return(baseSandbox, nil)
				repo.On("GetSnapshotByName", mock.Anything, mock.AnythingOfType("string")).Return((*model.Snapshot)(nil), model.ErrNotFound)
				eng.On("CreateSnapshot", mock.Anything, baseSandbox.ID, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(int64(1024), int64(512), nil)
				repo.On("CreateSnapshot", mock.Anything, mock.MatchedBy(func(snapshot model.Snapshot) bool {
					return snapshot.SourceSandboxID == baseSandbox.ID && snapshot.Name != ""
				})).Return(nil)
			},
		},
		"Creating snapshot with invalid name should fail": {
			req:    snapshotcreate.Request{NameOrID: baseSandbox.Name, SnapshotName: "bad name"},
			expErr: true,
			errIs:  model.ErrNotValid,
		},
		"Creating snapshot when sandbox is running should fail": {
			req: snapshotcreate.Request{NameOrID: baseSandbox.Name, SnapshotName: "snap-1"},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				sb := *baseSandbox
				sb.Status = model.SandboxStatusRunning
				repo.On("GetSandboxByName", mock.Anything, baseSandbox.Name).Return(&sb, nil)
			},
			expErr: true,
			errIs:  model.ErrNotValid,
		},
		"Creating snapshot with existing name should fail": {
			req: snapshotcreate.Request{NameOrID: baseSandbox.Name, SnapshotName: "snap-1"},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				repo.On("GetSandboxByName", mock.Anything, baseSandbox.Name).Return(baseSandbox, nil)
				repo.On("GetSnapshotByName", mock.Anything, "snap-1").Return(&model.Snapshot{ID: "existing"}, nil)
			},
			expErr: true,
			errIs:  model.ErrAlreadyExists,
		},
		"Creating snapshot with engine error should fail": {
			req: snapshotcreate.Request{NameOrID: baseSandbox.Name, SnapshotName: "snap-1"},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				repo.On("GetSandboxByName", mock.Anything, baseSandbox.Name).Return(baseSandbox, nil)
				repo.On("GetSnapshotByName", mock.Anything, "snap-1").Return((*model.Snapshot)(nil), model.ErrNotFound)
				eng.On("CreateSnapshot", mock.Anything, baseSandbox.ID, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(int64(0), int64(0), errors.New("boom"))
			},
			expErr: true,
		},
		"Creating snapshot when sandbox not found by name and id should fail": {
			req: snapshotcreate.Request{NameOrID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", SnapshotName: "snap-1"},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				repo.On("GetSandboxByName", mock.Anything, "01ARZ3NDEKTSV4RRFFQ69G5FAA").Return((*model.Sandbox)(nil), model.ErrNotFound)
				repo.On("GetSandbox", mock.Anything, "01ARZ3NDEKTSV4RRFFQ69G5FAA").Return((*model.Sandbox)(nil), model.ErrNotFound)
			},
			expErr: true,
			errIs:  model.ErrNotFound,
		},
		"Creating snapshot with repository persistence error should fail": {
			req: snapshotcreate.Request{NameOrID: baseSandbox.Name, SnapshotName: "snap-1"},
			setup: func(repo *storagemock.MockRepository, eng *sandboxmock.MockEngine) {
				repo.On("GetSandboxByName", mock.Anything, baseSandbox.Name).Return(baseSandbox, nil)
				repo.On("GetSnapshotByName", mock.Anything, "snap-1").Return((*model.Snapshot)(nil), model.ErrNotFound)
				eng.On("CreateSnapshot", mock.Anything, baseSandbox.ID, mock.AnythingOfType("string"), mock.AnythingOfType("string")).Return(int64(1024), int64(512), nil)
				repo.On("CreateSnapshot", mock.Anything, mock.AnythingOfType("model.Snapshot")).Return(errors.New("db error"))
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			repo := storagemock.NewMockRepository(t)
			eng := sandboxmock.NewMockEngine(t)

			if test.setup != nil {
				test.setup(repo, eng)
			}

			svc, err := snapshotcreate.NewService(snapshotcreate.ServiceConfig{
				Engine:     eng,
				Repository: repo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			snapshot, err := svc.Run(context.Background(), test.req)

			if test.expErr {
				assert.Error(err)
				assert.Nil(snapshot)
				if test.errIs != nil {
					assert.True(errors.Is(err, test.errIs))
				}
				return
			}

			require.NoError(err)
			require.NotNil(snapshot)
			assert.NotEmpty(snapshot.ID)
			if test.req.SnapshotName != "" {
				assert.Equal(test.req.SnapshotName, snapshot.Name)
			} else {
				assert.Regexp(`^sandbox1-\d{8}-\d{4}(-\d+)?$`, snapshot.Name)
			}
			assert.Equal(baseSandbox.ID, snapshot.SourceSandboxID)
			assert.WithinDuration(time.Now().UTC(), snapshot.CreatedAt, 2*time.Second)
		})
	}
}
