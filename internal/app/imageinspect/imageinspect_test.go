package imageinspect_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/imageinspect"
	"github.com/slok/sbx/internal/image/imagemock"
	"github.com/slok/sbx/internal/model"
)

func TestServiceRun(t *testing.T) {
	tests := map[string]struct {
		version       string
		mockResult    *model.ImageManifest
		mockErr       error
		snapExists    bool
		snapManifest  *model.ImageManifest
		useSnapMgr    bool
		expManifest   *model.ImageManifest
		expErr        bool
		expectImgCall bool // whether ImageManager.GetManifest should be called
	}{
		"Inspecting a remote release should use the image manager.": {
			version: "v0.1.0",
			mockResult: &model.ImageManifest{
				Version: "v0.1.0",
				Firecracker: model.FirecrackerInfo{
					Version: "v1.14.1",
				},
			},
			expectImgCall: true,
			expManifest: &model.ImageManifest{
				Version: "v0.1.0",
				Firecracker: model.FirecrackerInfo{
					Version: "v1.14.1",
				},
			},
		},

		"An error from the image manager should propagate.": {
			version:       "v99.0.0",
			mockErr:       fmt.Errorf("not found"),
			expectImgCall: true,
			expErr:        true,
		},

		"Inspecting a snapshot should return the snapshot manifest.": {
			version:    "my-snap",
			useSnapMgr: true,
			snapExists: true,
			snapManifest: &model.ImageManifest{
				Version: "my-snap",
				Snapshot: &model.SnapshotInfo{
					SourceSandboxName: "test-sb",
				},
			},
			expectImgCall: false,
			expManifest: &model.ImageManifest{
				Version: "my-snap",
				Snapshot: &model.SnapshotInfo{
					SourceSandboxName: "test-sb",
				},
			},
		},

		"When snapshot doesn't exist, should fall back to image manager.": {
			version:    "v0.2.0",
			useSnapMgr: true,
			snapExists: false,
			mockResult: &model.ImageManifest{
				Version: "v0.2.0",
			},
			expectImgCall: true,
			expManifest: &model.ImageManifest{
				Version: "v0.2.0",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			if tc.expectImgCall {
				mgr.On("GetManifest", mock.Anything, tc.version).Return(tc.mockResult, tc.mockErr)
			}

			cfg := imageinspect.ServiceConfig{Manager: mgr}

			if tc.useSnapMgr {
				snapMgr := imagemock.NewMockSnapshotManager(t)
				snapMgr.On("Exists", mock.Anything, tc.version).Return(tc.snapExists, nil)
				if tc.snapExists {
					snapMgr.On("GetManifest", mock.Anything, tc.version).Return(tc.snapManifest, nil)
				}
				cfg.SnapshotManager = snapMgr
			}

			svc, err := imageinspect.NewService(cfg)
			require.NoError(t, err)

			got, err := svc.Run(context.Background(), imageinspect.Request{Version: tc.version})
			if tc.expErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expManifest, got)
		})
	}
}
