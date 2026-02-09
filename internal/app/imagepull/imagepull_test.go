package imagepull_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/imagepull"
	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/image/imagemock"
)

func TestServiceRun(t *testing.T) {
	tests := map[string]struct {
		req        imagepull.Request
		mockResult *image.PullResult
		mockErr    error
		expResult  *image.PullResult
		expErr     bool
	}{
		"Successful pull should return result.": {
			req: imagepull.Request{Version: "v0.1.0"},
			mockResult: &image.PullResult{
				Version: "v0.1.0", KernelPath: "/k", RootFSPath: "/r", FirecrackerPath: "/f",
			},
			expResult: &image.PullResult{
				Version: "v0.1.0", KernelPath: "/k", RootFSPath: "/r", FirecrackerPath: "/f",
			},
		},
		"Skipped pull should return skipped result.": {
			req:        imagepull.Request{Version: "v0.1.0"},
			mockResult: &image.PullResult{Version: "v0.1.0", Skipped: true},
			expResult:  &image.PullResult{Version: "v0.1.0", Skipped: true},
		},
		"Pull with force should pass force option.": {
			req: imagepull.Request{Version: "v0.1.0", Force: true},
			mockResult: &image.PullResult{
				Version: "v0.1.0", KernelPath: "/k", RootFSPath: "/r", FirecrackerPath: "/f",
			},
			expResult: &image.PullResult{
				Version: "v0.1.0", KernelPath: "/k", RootFSPath: "/r", FirecrackerPath: "/f",
			},
		},
		"Error from puller should propagate.": {
			req:     imagepull.Request{Version: "v0.1.0"},
			mockErr: fmt.Errorf("download error"),
			expErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			puller := imagemock.NewMockImagePuller(t)
			puller.On("Pull", mock.Anything, tc.req.Version, mock.Anything).Return(tc.mockResult, tc.mockErr)

			svc, err := imagepull.NewService(imagepull.ServiceConfig{Puller: puller})
			require.NoError(t, err)

			got, err := svc.Run(context.Background(), tc.req)
			if tc.expErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expResult, got)
		})
	}
}
