package snapshotlist_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/snapshotlist"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config snapshotlist.ServiceConfig
		expErr bool
	}{
		"valid config should create service": {
			config: snapshotlist.ServiceConfig{
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},
		"missing repository should fail": {
			config: snapshotlist.ServiceConfig{
				Logger: log.Noop,
			},
			expErr: true,
		},
		"nil logger should default to noop": {
			config: snapshotlist.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			svc, err := snapshotlist.NewService(test.config)

			if test.expErr {
				require.Error(err)
				require.Nil(svc)
			} else {
				require.NoError(err)
				require.NotNil(svc)
			}
		})
	}
}

func TestService_Run(t *testing.T) {
	createdAt := time.Date(2026, 1, 30, 10, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		mock      func(m *storagemock.MockRepository)
		req       snapshotlist.Request
		expResult func() []model.Snapshot
		expErr    bool
	}{
		"list all snapshots": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSnapshots", mock.Anything).Once().Return([]model.Snapshot{
					{ID: "snap-1", Name: "snap-one", SourceSandboxName: "sb-1", VirtualSizeBytes: 10 * 1024 * 1024 * 1024, AllocatedSizeBytes: 700 * 1024 * 1024, CreatedAt: createdAt},
					{ID: "snap-2", Name: "snap-two", SourceSandboxName: "sb-2", VirtualSizeBytes: 5 * 1024 * 1024 * 1024, AllocatedSizeBytes: 300 * 1024 * 1024, CreatedAt: createdAt},
				}, nil)
			},
			req: snapshotlist.Request{},
			expResult: func() []model.Snapshot {
				return []model.Snapshot{
					{ID: "snap-1", Name: "snap-one", SourceSandboxName: "sb-1", VirtualSizeBytes: 10 * 1024 * 1024 * 1024, AllocatedSizeBytes: 700 * 1024 * 1024, CreatedAt: createdAt},
					{ID: "snap-2", Name: "snap-two", SourceSandboxName: "sb-2", VirtualSizeBytes: 5 * 1024 * 1024 * 1024, AllocatedSizeBytes: 300 * 1024 * 1024, CreatedAt: createdAt},
				}
			},
			expErr: false,
		},
		"empty repository returns empty list": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSnapshots", mock.Anything).Once().Return([]model.Snapshot{}, nil)
			},
			req: snapshotlist.Request{},
			expResult: func() []model.Snapshot {
				return []model.Snapshot{}
			},
			expErr: false,
		},
		"repository error should propagate": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSnapshots", mock.Anything).Once().Return(nil, fmt.Errorf("database error"))
			},
			req:       snapshotlist.Request{},
			expResult: nil,
			expErr:    true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Setup.
			m := &storagemock.MockRepository{}
			test.mock(m)

			svc, err := snapshotlist.NewService(snapshotlist.ServiceConfig{
				Repository: m,
				Logger:     log.Noop,
			})
			require.NoError(err)

			// Execute.
			result, err := svc.Run(context.Background(), test.req)

			// Verify.
			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				if test.expResult != nil {
					assert.Equal(test.expResult(), result)
				}
			}

			m.AssertExpectations(t)
		})
	}
}
