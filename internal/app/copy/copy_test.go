package copy

import (
	"context"
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

func TestParseCopyArgs(t *testing.T) {
	tests := map[string]struct {
		src       string
		dst       string
		expParsed *ParsedCopy
		expErr    bool
	}{
		"Host to sandbox (CopyTo)": {
			src: "./file.txt",
			dst: "my-sandbox:/workspace/",
			expParsed: &ParsedCopy{
				SandboxRef: "my-sandbox",
				LocalPath:  "./file.txt",
				RemotePath: "/workspace/",
				ToSandbox:  true,
			},
		},

		"Host directory to sandbox (CopyTo)": {
			src: "./project/",
			dst: "my-sandbox:/workspace/project/",
			expParsed: &ParsedCopy{
				SandboxRef: "my-sandbox",
				LocalPath:  "./project/",
				RemotePath: "/workspace/project/",
				ToSandbox:  true,
			},
		},

		"Sandbox to host (CopyFrom)": {
			src: "my-sandbox:/workspace/output.txt",
			dst: "./",
			expParsed: &ParsedCopy{
				SandboxRef: "my-sandbox",
				LocalPath:  "./",
				RemotePath: "/workspace/output.txt",
				ToSandbox:  false,
			},
		},

		"Sandbox directory to host (CopyFrom)": {
			src: "my-sandbox:/var/log/",
			dst: "./logs/",
			expParsed: &ParsedCopy{
				SandboxRef: "my-sandbox",
				LocalPath:  "./logs/",
				RemotePath: "/var/log/",
				ToSandbox:  false,
			},
		},

		"Using sandbox ID instead of name (CopyTo)": {
			src: "./file.txt",
			dst: "01KGB78ZKA:/workspace/",
			expParsed: &ParsedCopy{
				SandboxRef: "01KGB78ZKA",
				LocalPath:  "./file.txt",
				RemotePath: "/workspace/",
				ToSandbox:  true,
			},
		},

		"Using sandbox ID instead of name (CopyFrom)": {
			src: "01KGB78ZKA:/workspace/file.txt",
			dst: "./",
			expParsed: &ParsedCopy{
				SandboxRef: "01KGB78ZKA",
				LocalPath:  "./",
				RemotePath: "/workspace/file.txt",
				ToSandbox:  false,
			},
		},

		"Both arguments have colon should fail": {
			src:    "sandbox1:/path1",
			dst:    "sandbox2:/path2",
			expErr: true,
		},

		"Neither argument has colon should fail": {
			src:    "./file.txt",
			dst:    "./other.txt",
			expErr: true,
		},

		"Empty sandbox name should fail": {
			src:    "./file.txt",
			dst:    ":/path",
			expErr: true,
		},

		"Empty remote path should fail": {
			src:    "./file.txt",
			dst:    "sandbox:",
			expErr: true,
		},

		"Empty sandbox name in source should fail": {
			src:    ":/path",
			dst:    "./file.txt",
			expErr: true,
		},

		"Empty remote path in source should fail": {
			src:    "sandbox:",
			dst:    "./file.txt",
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			parsed, err := ParseCopyArgs(test.src, test.dst)

			if test.expErr {
				assert.Error(err)
				assert.Nil(parsed)
			} else {
				assert.NoError(err)
				assert.Equal(test.expParsed, parsed)
			}
		})
	}
}

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
	// Create a temp file for tests that need a real source file.
	tempDir := t.TempDir()
	existingFile := filepath.Join(tempDir, "existing.txt")
	err := os.WriteFile(existingFile, []byte("test content"), 0644)
	require.NoError(t, err)

	tests := map[string]struct {
		mock   func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository)
		req    Request
		expErr bool
	}{
		"CopyTo file on running sandbox should succeed": {
			req: Request{
				Source:      existingFile,
				Destination: "test-sandbox:/workspace/",
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)
				mEngine.On("CopyTo", mock.Anything, "test-id", existingFile, "/workspace/").Once().Return(nil)
			},
			expErr: false,
		},

		"CopyFrom file on running sandbox should succeed": {
			req: Request{
				Source:      "test-sandbox:/workspace/file.txt",
				Destination: tempDir,
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)
				mEngine.On("CopyFrom", mock.Anything, "test-id", "/workspace/file.txt", tempDir).Once().Return(nil)
			},
			expErr: false,
		},

		"CopyTo using sandbox ID should succeed": {
			req: Request{
				Source:      existingFile,
				Destination: "TEST-ID:/workspace/",
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				// Name lookup fails.
				mRepo.On("GetSandboxByName", mock.Anything, "TEST-ID").Once().Return(nil, model.ErrNotFound)

				// ID lookup succeeds.
				sandbox := &model.Sandbox{
					ID:     "TEST-ID",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandbox", mock.Anything, "TEST-ID").Once().Return(sandbox, nil)
				mEngine.On("CopyTo", mock.Anything, "TEST-ID", existingFile, "/workspace/").Once().Return(nil)
			},
			expErr: false,
		},

		"CopyTo with nonexistent source should fail": {
			req: Request{
				Source:      "/nonexistent/file.txt",
				Destination: "test-sandbox:/workspace/",
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				// No mocks needed - should fail before hitting repo/engine.
			},
			expErr: true,
		},

		"Sandbox not found should fail": {
			req: Request{
				Source:      existingFile,
				Destination: "nonexistent:/workspace/",
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				mRepo.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
				mRepo.On("GetSandbox", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			expErr: true,
		},

		"Stopped sandbox should fail": {
			req: Request{
				Source:      existingFile,
				Destination: "stopped-sandbox:/workspace/",
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

		"Engine CopyTo error should fail": {
			req: Request{
				Source:      existingFile,
				Destination: "test-sandbox:/workspace/",
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)
				mEngine.On("CopyTo", mock.Anything, "test-id", existingFile, "/workspace/").Once().Return(model.ErrNotFound)
			},
			expErr: true,
		},

		"Engine CopyFrom error should fail": {
			req: Request{
				Source:      "test-sandbox:/workspace/file.txt",
				Destination: tempDir,
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				sandbox := &model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Once().Return(sandbox, nil)
				mEngine.On("CopyFrom", mock.Anything, "test-id", "/workspace/file.txt", tempDir).Once().Return(model.ErrNotFound)
			},
			expErr: true,
		},

		"Invalid colon syntax should fail": {
			req: Request{
				Source:      "./file.txt",
				Destination: "./other.txt",
			},
			mock: func(mEngine *sandboxmock.MockEngine, mRepo *storagemock.MockRepository) {
				// No mocks needed - should fail during parsing.
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

			err = svc.Run(context.TODO(), test.req)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}

			mEngine.AssertExpectations(t)
			mRepo.AssertExpectations(t)
		})
	}
}
