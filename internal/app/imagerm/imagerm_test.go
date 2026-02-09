package imagerm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/imagerm"
	"github.com/slok/sbx/internal/image/imagemock"
)

func TestServiceRun(t *testing.T) {
	tests := map[string]struct {
		version       string
		mockErr       error
		useSnapMgr    bool
		snapExists    bool
		snapRemoveErr error
		expectImgCall bool // whether ImageManager.Remove should be called
		expErr        bool
	}{
		"Removing a release image should use the image manager.": {
			version:       "v0.1.0",
			expectImgCall: true,
		},

		"An error from the image manager should propagate.": {
			version:       "v0.1.0",
			mockErr:       fmt.Errorf("not installed"),
			expectImgCall: true,
			expErr:        true,
		},

		"Removing a snapshot should use the snapshot manager.": {
			version:       "my-snap",
			useSnapMgr:    true,
			snapExists:    true,
			expectImgCall: false,
		},

		"When snapshot doesn't exist, should fall back to image manager.": {
			version:       "v0.2.0",
			useSnapMgr:    true,
			snapExists:    false,
			expectImgCall: true,
		},

		"An error from snapshot remove should propagate.": {
			version:       "my-snap",
			useSnapMgr:    true,
			snapExists:    true,
			snapRemoveErr: fmt.Errorf("disk error"),
			expectImgCall: false,
			expErr:        true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			if tc.expectImgCall {
				mgr.On("Remove", mock.Anything, tc.version).Return(tc.mockErr)
			}

			cfg := imagerm.ServiceConfig{Manager: mgr}

			if tc.useSnapMgr {
				snapMgr := imagemock.NewMockSnapshotManager(t)
				snapMgr.On("Exists", mock.Anything, tc.version).Return(tc.snapExists, nil)
				if tc.snapExists {
					snapMgr.On("Remove", mock.Anything, tc.version).Return(tc.snapRemoveErr)
				}
				cfg.SnapshotManager = snapMgr
			}

			svc, err := imagerm.NewService(cfg)
			require.NoError(t, err)

			err = svc.Run(context.Background(), imagerm.Request{Version: tc.version})
			if tc.expErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
