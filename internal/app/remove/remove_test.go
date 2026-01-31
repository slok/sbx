package remove_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/remove"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config remove.ServiceConfig
		expErr bool
	}{
		"valid config": {
			config: remove.ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
		"missing engine": {
			config: remove.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			svc, err := remove.NewService(test.config)
			if test.expErr {
				require.Error(err)
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
	stoppedAt := time.Date(2026, 1, 30, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		mockRepo   func(m *storagemock.MockRepository)
		mockEngine func(m *sandboxmock.MockEngine)
		req        remove.Request
		expErr     bool
	}{
		"remove stopped sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusStopped,
					CreatedAt: createdAt,
					StoppedAt: &stoppedAt,
				}, nil)
				m.On("DeleteSandbox", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Remove", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			req:    remove.Request{NameOrID: "my-sandbox"},
			expErr: false,
		},
		"cannot remove running sandbox without force": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {},
			req:        remove.Request{NameOrID: "my-sandbox", Force: false},
			expErr:     true,
		},
		"force remove running sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
				m.On("DeleteSandbox", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Stop", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
				m.On("Remove", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			req:    remove.Request{NameOrID: "my-sandbox", Force: true},
			expErr: false,
		},
		"sandbox not found": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {},
			req:        remove.Request{NameOrID: "nonexistent"},
			expErr:     true,
		},
		"engine error propagates": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusStopped,
					CreatedAt: createdAt,
					StoppedAt: &stoppedAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Remove", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(fmt.Errorf("engine error"))
			},
			req:    remove.Request{NameOrID: "my-sandbox"},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mRepo := &storagemock.MockRepository{}
			mEngine := &sandboxmock.MockEngine{}
			test.mockRepo(mRepo)
			test.mockEngine(mEngine)

			svc, err := remove.NewService(remove.ServiceConfig{
				Engine:     mEngine,
				Repository: mRepo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			result, err := svc.Run(context.Background(), test.req)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.NotNil(result)
			}

			mRepo.AssertExpectations(t)
			mEngine.AssertExpectations(t)
		})
	}
}
