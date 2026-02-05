package start_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/start"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config start.ServiceConfig
		expErr bool
	}{
		"valid config should create service": {
			config: start.ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},
		"missing engine should fail": {
			config: start.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: true,
		},
		"missing repository should fail": {
			config: start.ServiceConfig{
				Engine: &sandboxmock.MockEngine{},
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			svc, err := start.NewService(test.config)
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
		req        start.Request
		expErr     bool
	}{
		"start stopped sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusStopped,
					CreatedAt: createdAt,
					StartedAt: &startedAt,
					StoppedAt: &stoppedAt,
				}, nil)
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.Status == model.SandboxStatusStopped && s.SessionConfig.Env != nil
				})).Once().Return(nil)
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.Status == model.SandboxStatusRunning && s.StartedAt != nil
				})).Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Start", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
				m.On("Exec", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", []string{"mkdir", "-p", "/etc/sbx", "/etc/profile.d", "/root/.ssh"}, mock.Anything).Once().Return(&model.ExecResult{}, nil)
				m.On("CopyTo", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", mock.Anything, "/etc/sbx/session-env.sh").Once().Return(nil)
				m.On("CopyTo", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", mock.Anything, "/etc/profile.d/sbx-session-env.sh").Once().Return(nil)
				m.On("CopyTo", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", mock.Anything, "/root/.ssh/rc").Once().Return(nil)
				m.On("Exec", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", []string{"chmod", "644", "/etc/sbx/session-env.sh", "/etc/profile.d/sbx-session-env.sh"}, mock.Anything).Once().Return(&model.ExecResult{}, nil)
				m.On("Exec", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", []string{"chmod", "700", "/root/.ssh/rc"}, mock.Anything).Once().Return(&model.ExecResult{}, nil)
			},
			req:    start.Request{NameOrID: "my-sandbox"},
			expErr: false,
		},
		"cannot start already running sandbox": {
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
			req:        start.Request{NameOrID: "my-sandbox"},
			expErr:     true,
		},
		"start created sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusCreated,
					CreatedAt: createdAt,
				}, nil)
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.Status == model.SandboxStatusCreated && s.SessionConfig.Env != nil
				})).Once().Return(nil)
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.Status == model.SandboxStatusRunning && s.StartedAt != nil
				})).Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Start", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(nil)
				m.On("Exec", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", []string{"mkdir", "-p", "/etc/sbx", "/etc/profile.d", "/root/.ssh"}, mock.Anything).Once().Return(&model.ExecResult{}, nil)
				m.On("CopyTo", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", mock.Anything, "/etc/sbx/session-env.sh").Once().Return(nil)
				m.On("CopyTo", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", mock.Anything, "/etc/profile.d/sbx-session-env.sh").Once().Return(nil)
				m.On("CopyTo", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", mock.Anything, "/root/.ssh/rc").Once().Return(nil)
				m.On("Exec", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", []string{"chmod", "644", "/etc/sbx/session-env.sh", "/etc/profile.d/sbx-session-env.sh"}, mock.Anything).Once().Return(&model.ExecResult{}, nil)
				m.On("Exec", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH", []string{"chmod", "700", "/root/.ssh/rc"}, mock.Anything).Once().Return(&model.ExecResult{}, nil)
			},
			req:    start.Request{NameOrID: "my-sandbox"},
			expErr: false,
		},
		"cannot start pending sandbox": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "my-sandbox").Once().Return(&model.Sandbox{
					ID:        "01H2QWERTYASDFGZXCVBNMLKJH",
					Name:      "my-sandbox",
					Status:    model.SandboxStatusPending,
					CreatedAt: createdAt,
				}, nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {},
			req:        start.Request{NameOrID: "my-sandbox"},
			expErr:     true,
		},
		"sandbox not found": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {},
			req:        start.Request{NameOrID: "nonexistent"},
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
				m.On("UpdateSandbox", mock.Anything, mock.MatchedBy(func(s model.Sandbox) bool {
					return s.Status == model.SandboxStatusStopped && s.SessionConfig.Env != nil
				})).Once().Return(nil)
			},
			mockEngine: func(m *sandboxmock.MockEngine) {
				m.On("Start", mock.Anything, "01H2QWERTYASDFGZXCVBNMLKJH").Once().Return(fmt.Errorf("engine error"))
			},
			req:    start.Request{NameOrID: "my-sandbox"},
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

			svc, err := start.NewService(start.ServiceConfig{
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
				assert.Equal(model.SandboxStatusRunning, result.Status)
			}

			mRepo.AssertExpectations(t)
			mEngine.AssertExpectations(t)
		})
	}
}
