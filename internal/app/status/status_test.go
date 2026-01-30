package status_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/status"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config status.ServiceConfig
		expErr bool
	}{
		"valid config should create service": {
			config: status.ServiceConfig{
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},
		"missing repository should fail": {
			config: status.ServiceConfig{
				Logger: log.Noop,
			},
			expErr: true,
		},
		"nil logger should default to noop": {
			config: status.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			svc, err := status.NewService(test.config)

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

	tests := map[string]struct {
		mock      func(m *storagemock.MockRepository)
		req       status.Request
		expResult func() *model.Sandbox
		expErr    bool
	}{
		"get sandbox by name": {
			mock: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
			},
			req: status.Request{NameOrID: "my-sandbox"},
			expResult: func() *model.Sandbox {
				return &model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}
			},
			expErr: false,
		},
		"get sandbox by ID when name not found": {
			mock: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil, model.ErrNotFound)
				m.On("GetSandbox", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
			},
			req: status.Request{NameOrID: "01H2QWERTYASDFGZXCVBNMLKJH"},
			expResult: func() *model.Sandbox {
				return &model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}
			},
			expErr: false,
		},
		"sandbox not found by name (short string, not ULID-like)": {
			mock: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			req:       status.Request{NameOrID: "nonexistent"},
			expResult: nil,
			expErr:    true,
		},
		"sandbox not found by name or ID": {
			mock: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil, model.ErrNotFound)
				m.On("GetSandbox", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil, model.ErrNotFound)
			},
			req:       status.Request{NameOrID: "01H2QWERTYASDFGZXCVBNMLKJH"},
			expResult: nil,
			expErr:    true,
		},
		"repository error should propagate": {
			mock: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(nil, fmt.Errorf("database error"))
			},
			req:       status.Request{NameOrID: "my-sandbox"},
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

			svc, err := status.NewService(status.ServiceConfig{
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
