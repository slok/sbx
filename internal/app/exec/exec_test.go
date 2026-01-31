package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		cfg    ServiceConfig
		expErr bool
	}{
		"Valid configuration should create service successfully": {
			cfg: ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: false,
		},

		"Missing engine should fail": {
			cfg: ServiceConfig{
				Repository: &storagemock.MockRepository{},
				Logger:     log.Noop,
			},
			expErr: true,
		},

		"Missing repository should fail": {
			cfg: ServiceConfig{
				Engine: &sandboxmock.MockEngine{},
				Logger: log.Noop,
			},
			expErr: true,
		},

		"Missing logger should use noop logger": {
			cfg: ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			svc, err := NewService(test.cfg)

			if test.expErr {
				assert.Error(err)
				assert.Nil(svc)
			} else {
				assert.NoError(err)
				assert.NotNil(svc)
			}
		})
	}
}

func TestServiceRun(t *testing.T) {
	tests := map[string]struct {
		mock   func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository)
		req    Request
		expErr bool
		expRes *model.ExecResult
	}{
		"Executing command on running sandbox should succeed": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{"echo", "hello"},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				// Sandbox exists and is running
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// Exec succeeds
				result := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"echo", "hello"}, mock.Anything).Once().Return(result, nil)
			},
			expRes: &model.ExecResult{ExitCode: 0},
			expErr: false,
		},

		"Executing command with non-zero exit code should succeed and return exit code": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{"false"},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				result := &model.ExecResult{ExitCode: 1}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"false"}, mock.Anything).Once().Return(result, nil)
			},
			expRes: &model.ExecResult{ExitCode: 1},
			expErr: false,
		},

		"Executing with working directory should pass options to engine": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{"pwd"},
				Opts:     model.ExecOpts{WorkingDir: "/app"},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				result := &model.ExecResult{ExitCode: 0}
				// Verify working directory is passed
				mEngine.On("Exec", mock.Anything, "test-id", []string{"pwd"}, mock.MatchedBy(func(opts model.ExecOpts) bool {
					return opts.WorkingDir == "/app"
				})).Once().Return(result, nil)
			},
			expRes: &model.ExecResult{ExitCode: 0},
			expErr: false,
		},

		"Executing with environment variables should pass options to engine": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{"env"},
				Opts:     model.ExecOpts{Env: map[string]string{"FOO": "bar"}},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				result := &model.ExecResult{ExitCode: 0}
				// Verify env vars are passed
				mEngine.On("Exec", mock.Anything, "test-id", []string{"env"}, mock.MatchedBy(func(opts model.ExecOpts) bool {
					return opts.Env["FOO"] == "bar"
				})).Once().Return(result, nil)
			},
			expRes: &model.ExecResult{ExitCode: 0},
			expErr: false,
		},

		"Executing with streams should pass options to engine": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{"cat"},
				Opts: model.ExecOpts{
					Stdin:  bytes.NewBufferString("test input"),
					Stdout: &bytes.Buffer{},
					Stderr: &bytes.Buffer{},
				},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				result := &model.ExecResult{ExitCode: 0}
				// Verify streams are passed
				mEngine.On("Exec", mock.Anything, "test-id", []string{"cat"}, mock.MatchedBy(func(opts model.ExecOpts) bool {
					return opts.Stdin != nil && opts.Stdout != nil && opts.Stderr != nil
				})).Once().Return(result, nil)
			},
			expRes: &model.ExecResult{ExitCode: 0},
			expErr: false,
		},

		"Empty command should fail": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				// No mocks needed - should fail before hitting repo/engine
			},
			expErr: true,
		},

		"Sandbox not found by name should try by ID": {
			req: Request{
				NameOrID: "test-id",
				Command:  []string{"echo", "hello"},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				// Name lookup fails
				mRepo.On("GetSandboxByName", mock.Anything, "test-id").Once().Return(nil, model.ErrNotFound)

				// ID lookup succeeds
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandbox", mock.Anything, "test-id").Once().Return(sandbox, nil)

				result := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"echo", "hello"}, mock.Anything).Once().Return(result, nil)
			},
			expRes: &model.ExecResult{ExitCode: 0},
			expErr: false,
		},

		"Sandbox not found should fail": {
			req: Request{
				NameOrID: "nonexistent",
				Command:  []string{"echo", "hello"},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				mRepo.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
				mRepo.On("GetSandbox", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			expErr: true,
		},

		"Stopped sandbox should fail": {
			req: Request{
				NameOrID: "stopped-sandbox",
				Command:  []string{"echo", "hello"},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "stopped-sandbox",
					Status: model.SandboxStatusStopped,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "stopped-sandbox").Once().Return(sandbox, nil)
			},
			expErr: true,
		},

		"Engine exec error should fail": {
			req: Request{
				NameOrID: "test-sandbox",
				Command:  []string{"invalid"},
				Opts:     model.ExecOpts{},
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				mEngine.On("Exec", mock.Anything, "test-id", []string{"invalid"}, mock.Anything).Once().Return(nil, fmt.Errorf("exec failed"))
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mEngine := &sandboxmock.MockEngine{}
			mRepo := &storagemock.MockRepository{}
			test.mock(mEngine, mRepo)

			svc, err := NewService(ServiceConfig{
				Engine:     mEngine,
				Repository: mRepo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			result, err := svc.Run(context.TODO(), test.req)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.Equal(test.expRes, result)
			}

			mEngine.AssertExpectations(t)
			mRepo.AssertExpectations(t)
		})
	}
}

// Test helper to verify stdout/stderr output
func TestServiceRunWithOutput(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	mEngine := &sandboxmock.MockEngine{}
	mRepo := &storagemock.MockRepository{}

	sandbox := &model.Sandbox{
		ID:     "test-id",
		Name:   "test-sandbox",
		Status: model.SandboxStatusRunning,
	}
	mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

	// Mock engine that writes to stdout
	mEngine.On("Exec", mock.Anything, "test-id", []string{"echo", "hello"}, mock.Anything).Once().
		Run(func(args mock.Arguments) {
			opts := args.Get(3).(model.ExecOpts)
			if opts.Stdout != nil {
				io.WriteString(opts.Stdout, "hello\n")
			}
		}).
		Return(&model.ExecResult{ExitCode: 0}, nil)

	svc, err := NewService(ServiceConfig{
		Engine:     mEngine,
		Repository: mRepo,
		Logger:     log.Noop,
	})
	require.NoError(err)

	stdout := &bytes.Buffer{}
	result, err := svc.Run(context.TODO(), Request{
		NameOrID: "test-sandbox",
		Command:  []string{"echo", "hello"},
		Opts:     model.ExecOpts{Stdout: stdout},
	})

	assert.NoError(err)
	assert.Equal(0, result.ExitCode)
	assert.Equal("hello\n", stdout.String())

	mEngine.AssertExpectations(t)
	mRepo.AssertExpectations(t)
}
