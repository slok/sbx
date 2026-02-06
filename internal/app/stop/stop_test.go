package stop_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/stop"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config stop.ServiceConfig
		expErr bool
	}{
		"valid config should create service": {
			config: stop.ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},
		"missing engine should fail": {
			config: stop.ServiceConfig{
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: true,
		},
		"missing repository should fail": {
			config: stop.ServiceConfig{
				Engine: &sandboxmock.MockEngine{},
				Logger: log.Noop,
			},
			expErr: true,
		},
		"nil logger should default to noop": {
			config: stop.ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			svc, err := stop.NewService(test.config)

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
		mockRepo   func(m *storagemock.MockRepository)
		mockEngine func(m *sandboxmock.MockEngine)
		req        stop.Request
		expErr     bool
	}{
		"stop running sandbox by name": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.ID == "01H2QWERTYASDFGZXCVBNMLKJH" &&
						s.Status == model.SandboxStatusStopped &&
						s.StoppedAt != nil
				})).Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Stop", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			req:    stop.Request{NameOrID: "my-sandbox"},
			expErr: false,
		},
		"stop running sandbox by ID": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil, model.ErrNotFound)
				m.On("GetSandbox", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.Status == model.SandboxStatusStopped
				})).Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Stop", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			req:    stop.Request{NameOrID: "01H2QWERTYASDFGZXCVBNMLKJH"},
			expErr: false,
		},
		"cannot stop already stopped sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				stoppedAt := time.Now().UTC()
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusStopped,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
					StoppedAt: &stoppedAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				// Engine stop should not be called
			},
			req:    stop.Request{NameOrID: "my-sandbox"},
			expErr: true,
		},
		"cannot stop pending sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusPending,
					CreatedAt: createdAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				// Engine stop should not be called
			},
			req:    stop.Request{NameOrID: "my-sandbox"},
			expErr: true,
		},
		"cannot stop failed sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusFailed,
					CreatedAt: createdAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				// Engine stop should not be called
			},
			req:    stop.Request{NameOrID: "my-sandbox"},
			expErr: true,
		},
		"sandbox not found should error": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				// Engine stop should not be called
			},
			req:    stop.Request{NameOrID: "nonexistent"},
			expErr: true,
		},
		"engine error should propagate": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Stop", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(fmt.Errorf("engine error"))
			},
			req:    stop.Request{NameOrID: "my-sandbox"},
			expErr: true,
		},
		"repository update error should propagate": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusRunning,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
				}, nil)
				m.On("UpdateSandbox", mock.Anything, mock.Anything).Once().Return(fmt.Errorf("database error"))
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Stop", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
			},
			req:    stop.Request{NameOrID: "my-sandbox"},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Setup
			mRepo := &storagemock.MockRepository{}
			mEngine := &sandboxmock.MockEngine{}
			test.mockRepo(mRepo)
			test.mockEngine(mEngine)

			svc, err := stop.NewService(stop.ServiceConfig{
				Engine:     mEngine,
				Repository: mRepo,
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
				assert.NotNil(result)
				assert.Equal(model.SandboxStatusStopped, result.Status)
				assert.NotNil(result.StoppedAt)
			}

			mRepo.AssertExpectations(t)
			mEngine.AssertExpectations(t)
		})
	}
}
