package forward_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/forward"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestServiceConfigValidation(t *testing.T) {
	tests := map[string]struct {
		config forward.ServiceConfig
		expErr bool
	}{
		"Valid config should not fail.": {
			config: forward.ServiceConfig{
				Engine:     &sandboxmock.MockEngine{},
				Repository: &storagemock.MockRepository{},
			},
		},
		"Missing engine should fail.": {
			config: forward.ServiceConfig{
				Repository: &storagemock.MockRepository{},
			},
			expErr: true,
		},
		"Missing repository should fail.": {
			config: forward.ServiceConfig{
				Engine: &sandboxmock.MockEngine{},
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			_, err := forward.NewService(test.config)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestServiceRun(t *testing.T) {
	tests := map[string]struct {
		mock   func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine)
		req    forward.Request
		expErr bool
	}{
		"Empty ports should fail.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports:    []model.PortMapping{},
			},
			expErr: true,
		},
		"Sandbox not found by name should try ID and fail if not found.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(nil, model.ErrNotFound)
				mRepo.On("GetSandbox", mock.Anything, "test-sandbox").Return(nil, model.ErrNotFound)
			},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports:    []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}},
			},
			expErr: true,
		},
		"Sandbox not running should fail.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusStopped,
				}, nil)
			},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports:    []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}},
			},
			expErr: true,
		},
		"Engine Forward error should fail.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}, nil)
				mEngine.On("Forward", mock.Anything, "test-id", []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}}).
					Return(fmt.Errorf("forward error"))
			},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports:    []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}},
			},
			expErr: true,
		},
		"Context cancellation should return nil (expected behavior).": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}, nil)
				mEngine.On("Forward", mock.Anything, "test-id", []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}}).
					Return(context.Canceled)
			},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports:    []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}},
			},
			expErr: false,
		},
		"Successful forward should return nil.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}, nil)
				mEngine.On("Forward", mock.Anything, "test-id", []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}}).
					Return(nil)
			},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports:    []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}},
			},
			expErr: false,
		},
		"Multiple ports should be passed to engine.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "test-sandbox").Return(&model.Sandbox{
					ID:     "test-id",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}, nil)
				ports := []model.PortMapping{
					{LocalPort: 8080, RemotePort: 8080},
					{LocalPort: 3000, RemotePort: 3000},
					{LocalPort: 9000, RemotePort: 5432},
				}
				mEngine.On("Forward", mock.Anything, "test-id", ports).Return(nil)
			},
			req: forward.Request{
				NameOrID: "test-sandbox",
				Ports: []model.PortMapping{
					{LocalPort: 8080, RemotePort: 8080},
					{LocalPort: 3000, RemotePort: 3000},
					{LocalPort: 9000, RemotePort: 5432},
				},
			},
			expErr: false,
		},
		"Looking up by ID when name not found should work.": {
			mock: func(mRepo *storagemock.MockRepository, mEngine *sandboxmock.MockEngine) {
				mRepo.On("GetSandboxByName", mock.Anything, "01ABC123").Return(nil, model.ErrNotFound)
				mRepo.On("GetSandbox", mock.Anything, "01ABC123").Return(&model.Sandbox{
					ID:     "01ABC123",
					Name:   "test-sandbox",
					Status: model.SandboxStatusRunning,
				}, nil)
				mEngine.On("Forward", mock.Anything, "01ABC123", []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}}).
					Return(nil)
			},
			req: forward.Request{
				NameOrID: "01ABC123",
				Ports:    []model.PortMapping{{LocalPort: 8080, RemotePort: 8080}},
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mRepo := &storagemock.MockRepository{}
			mEngine := &sandboxmock.MockEngine{}
			test.mock(mRepo, mEngine)

			svc, err := forward.NewService(forward.ServiceConfig{
				Engine:     mEngine,
				Repository: mRepo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			err = svc.Run(context.Background(), test.req)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}

			mRepo.AssertExpectations(t)
			mEngine.AssertExpectations(t)
		})
	}
}
