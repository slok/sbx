package list_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/list"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config list.ServiceConfig
		expErr bool
	}{
		"valid config should create service": {
			config: list.ServiceConfig{
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},
		"missing repository should fail": {
			config: list.ServiceConfig{
				Logger: log.Noop,
			},
			expErr: true,
		},
		"nil logger should default to noop": {
			config: list.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			svc, err := list.NewService(test.config)

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
	startedAt := time.Date(2026, 1, 30, 10, 0, 5, 0, time.UTC)

	running := model.SandboxStatusRunning
	stopped := model.SandboxStatusStopped

	tests := map[string]struct {
		mock      func(m *storagemock.MockRepository)
		req       list.Request
		expResult func() []model.Sandbox
		expErr    bool
	}{
		"list all sandboxes without filter": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSandboxes", mock.Anything).Once().Return([]model.Sandbox{
					{ID: "id1", Name: "sandbox-1", Status: model.SandboxStatusRunning, CreatedAt: createdAt},
					{ID: "id2", Name: "sandbox-2", Status: model.SandboxStatusStopped, CreatedAt: createdAt},
				}, nil)
			},
			req: list.Request{},
			expResult: func() []model.Sandbox {
				return []model.Sandbox{
					{ID: "id1", Name: "sandbox-1", Status: model.SandboxStatusRunning, CreatedAt: createdAt},
					{ID: "id2", Name: "sandbox-2", Status: model.SandboxStatusStopped, CreatedAt: createdAt},
				}
			},
			expErr: false,
		},
		"filter by running status": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSandboxes", mock.Anything).Once().Return([]model.Sandbox{
					{ID: "id1", Name: "sandbox-1", Status: model.SandboxStatusRunning, CreatedAt: createdAt, StartedAt: &startedAt},
					{ID: "id2", Name: "sandbox-2", Status: model.SandboxStatusStopped, CreatedAt: createdAt},
					{ID: "id3", Name: "sandbox-3", Status: model.SandboxStatusRunning, CreatedAt: createdAt, StartedAt: &startedAt},
				}, nil)
			},
			req: list.Request{StatusFilter: &running},
			expResult: func() []model.Sandbox {
				return []model.Sandbox{
					{ID: "id1", Name: "sandbox-1", Status: model.SandboxStatusRunning, CreatedAt: createdAt, StartedAt: &startedAt},
					{ID: "id3", Name: "sandbox-3", Status: model.SandboxStatusRunning, CreatedAt: createdAt, StartedAt: &startedAt},
				}
			},
			expErr: false,
		},
		"filter by stopped status": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSandboxes", mock.Anything).Once().Return([]model.Sandbox{
					{ID: "id1", Name: "sandbox-1", Status: model.SandboxStatusRunning, CreatedAt: createdAt},
					{ID: "id2", Name: "sandbox-2", Status: model.SandboxStatusStopped, CreatedAt: createdAt},
				}, nil)
			},
			req: list.Request{StatusFilter: &stopped},
			expResult: func() []model.Sandbox {
				return []model.Sandbox{
					{ID: "id2", Name: "sandbox-2", Status: model.SandboxStatusStopped, CreatedAt: createdAt},
				}
			},
			expErr: false,
		},
		"filter with no matches returns empty list": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSandboxes", mock.Anything).Once().Return([]model.Sandbox{
					{ID: "id1", Name: "sandbox-1", Status: model.SandboxStatusRunning, CreatedAt: createdAt},
				}, nil)
			},
			req: list.Request{StatusFilter: &stopped},
			expResult: func() []model.Sandbox {
				return []model.Sandbox{}
			},
			expErr: false,
		},
		"empty repository returns empty list": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSandboxes", mock.Anything).Once().Return([]model.Sandbox{}, nil)
			},
			req: list.Request{},
			expResult: func() []model.Sandbox {
				return []model.Sandbox{}
			},
			expErr: false,
		},
		"repository error should propagate": {
			mock: func(m *storagemock.MockRepository) {
				m.On("ListSandboxes", mock.Anything).Once().Return(nil, fmt.Errorf("database error"))
			},
			req:       list.Request{},
			expResult: nil,
			expErr:    true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Setup
			m := &storagemock.MockRepository{}
			test.mock(m)

			svc, err := list.NewService(list.ServiceConfig{
				Repository: m,
				Logger:     log.Noop,
			})
			require.NoError(err)

			// Execute
			result, err := svc.Run(context.Background(), test.req)

			// Verify
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
