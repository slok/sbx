package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// Test helper to verify stdout/stderr output.
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
				_, _ = io.WriteString(opts.Stdout, "hello\n")
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

func TestServiceRunWithFiles(t *testing.T) {
	// Helper to create a temp file that exists on disk.
	createTempFile := func(t *testing.T, name string) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), name)
		require.NoError(t, err)
		f.Close()
		return f.Name()
	}

	tests := map[string]struct {
		mock   func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request
		expErr bool
		expRes *model.ExecResult
	}{
		"Single file upload with workdir should create dir, upload, then exec": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				tmpFile := createTempFile(t, "script.sh")

				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// Expect mkdir -p for the workdir.
				mkdirResult := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"mkdir", "-p", "/app"}, mock.Anything).Once().Return(mkdirResult, nil)

				// Expect CopyTo with workdir destination.
				expectedRemote := filepath.Join("/app", filepath.Base(tmpFile))
				mEngine.On("CopyTo", mock.Anything, "test-id", tmpFile, expectedRemote).Once().Return(nil)

				// Then the actual exec.
				result := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"bash", "script.sh"}, mock.Anything).Once().Return(result, nil)

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"bash", "script.sh"},
					Files:    []string{tmpFile},
					Opts:     model.ExecOpts{WorkingDir: "/app"},
				}
			},
			expRes: &model.ExecResult{ExitCode: 0},
		},

		"Single file upload without workdir should create root dir, upload, then exec": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				tmpFile := createTempFile(t, "data.txt")

				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// Expect mkdir -p for "/" (no-op but consistent).
				mkdirResult := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"mkdir", "-p", "/"}, mock.Anything).Once().Return(mkdirResult, nil)

				// Expect CopyTo with "/" destination.
				expectedRemote := filepath.Join("/", filepath.Base(tmpFile))
				mEngine.On("CopyTo", mock.Anything, "test-id", tmpFile, expectedRemote).Once().Return(nil)

				result := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"cat", "data.txt"}, mock.Anything).Once().Return(result, nil)

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"cat", "data.txt"},
					Files:    []string{tmpFile},
					Opts:     model.ExecOpts{},
				}
			},
			expRes: &model.ExecResult{ExitCode: 0},
		},

		"Multiple files should create dir, upload all, then exec": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				tmpFile1 := createTempFile(t, "a.sh")
				tmpFile2 := createTempFile(t, "b.txt")

				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// Expect mkdir -p for /tmp.
				mkdirResult := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"mkdir", "-p", "/tmp"}, mock.Anything).Once().Return(mkdirResult, nil)

				// Both files uploaded to /tmp.
				mEngine.On("CopyTo", mock.Anything, "test-id", tmpFile1, filepath.Join("/tmp", filepath.Base(tmpFile1))).Once().Return(nil)
				mEngine.On("CopyTo", mock.Anything, "test-id", tmpFile2, filepath.Join("/tmp", filepath.Base(tmpFile2))).Once().Return(nil)

				result := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"ls"}, mock.Anything).Once().Return(result, nil)

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"ls"},
					Files:    []string{tmpFile1, tmpFile2},
					Opts:     model.ExecOpts{WorkingDir: "/tmp"},
				}
			},
			expRes: &model.ExecResult{ExitCode: 0},
		},

		"File upload failure should stop and not exec": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				tmpFile := createTempFile(t, "fail.sh")

				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// mkdir -p succeeds.
				mkdirResult := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"mkdir", "-p", "/app"}, mock.Anything).Once().Return(mkdirResult, nil)

				// CopyTo fails.
				mEngine.On("CopyTo", mock.Anything, "test-id", tmpFile, mock.Anything).Once().Return(fmt.Errorf("scp failed"))

				// User exec should NOT be called.

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"bash", "fail.sh"},
					Files:    []string{tmpFile},
					Opts:     model.ExecOpts{WorkingDir: "/app"},
				}
			},
			expErr: true,
		},

		"mkdir -p failure should stop before any uploads": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				tmpFile := createTempFile(t, "wont-upload.sh")

				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// mkdir -p fails.
				mEngine.On("Exec", mock.Anything, "test-id", []string{"mkdir", "-p", "/app"}, mock.Anything).Once().Return(nil, fmt.Errorf("mkdir failed"))

				// No CopyTo or user exec expected.

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"bash", "wont-upload.sh"},
					Files:    []string{tmpFile},
					Opts:     model.ExecOpts{WorkingDir: "/app"},
				}
			},
			expErr: true,
		},

		"Non-existent local file should fail before any engine call": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				// No engine calls expected.

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"bash", "nope.sh"},
					Files:    []string{"/nonexistent/path/nope.sh"},
					Opts:     model.ExecOpts{},
				}
			},
			expErr: true,
		},

		"Empty files list should exec without any uploads": {
			mock: func(t *testing.T, mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) Request {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)

				result := &model.ExecResult{ExitCode: 0}
				mEngine.On("Exec", mock.Anything, "test-id", []string{"echo", "hi"}, mock.Anything).Once().Return(result, nil)

				// No CopyTo expected.

				return Request{
					NameOrID: "test-sandbox",
					Command:  []string{"echo", "hi"},
					Files:    []string{},
					Opts:     model.ExecOpts{},
				}
			},
			expRes: &model.ExecResult{ExitCode: 0},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mEngine := &sandboxmock.MockEngine{}
			mRepo := &storagemock.MockRepository{}
			req := test.mock(t, mEngine, mRepo)

			svc, err := NewService(ServiceConfig{
				Engine:     mEngine,
				Repository: mRepo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			result, err := svc.Run(context.TODO(), req)

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
