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
		version     string
		mockResult  *model.ImageManifest
		mockErr     error
		expManifest *model.ImageManifest
		expErr      bool
	}{
		"Inspecting a local release image should return its manifest.": {
			version: "v0.1.0",
			mockResult: &model.ImageManifest{
				Version: "v0.1.0",
				Firecracker: model.FirecrackerInfo{
					Version: "v1.14.1",
				},
			},
			expManifest: &model.ImageManifest{
				Version: "v0.1.0",
				Firecracker: model.FirecrackerInfo{
					Version: "v1.14.1",
				},
			},
		},

		"Inspecting a local snapshot image should return its manifest.": {
			version: "my-snap",
			mockResult: &model.ImageManifest{
				Version: "my-snap",
				Snapshot: &model.SnapshotInfo{
					SourceSandboxName: "test-sb",
				},
			},
			expManifest: &model.ImageManifest{
				Version: "my-snap",
				Snapshot: &model.SnapshotInfo{
					SourceSandboxName: "test-sb",
				},
			},
		},

		"An error from the image manager should propagate.": {
			version: "v99.0.0",
			mockErr: fmt.Errorf("not found"),
			expErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			mgr.On("GetManifest", mock.Anything, tc.version).Return(tc.mockResult, tc.mockErr)

			svc, err := imageinspect.NewService(imageinspect.ServiceConfig{Manager: mgr})
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
