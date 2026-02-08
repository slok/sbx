package imagelist_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/imagelist"
	"github.com/slok/sbx/internal/image/imagemock"
	"github.com/slok/sbx/internal/model"
)

func TestServiceRun(t *testing.T) {
	tests := map[string]struct {
		mockReleases []model.ImageRelease
		mockErr      error
		expReleases  []model.ImageRelease
		expErr       bool
	}{
		"successful list": {
			mockReleases: []model.ImageRelease{
				{Version: "v0.2.0", Installed: false},
				{Version: "v0.1.0", Installed: true},
			},
			expReleases: []model.ImageRelease{
				{Version: "v0.2.0", Installed: false},
				{Version: "v0.1.0", Installed: true},
			},
		},
		"empty list": {
			mockReleases: []model.ImageRelease{},
			expReleases:  []model.ImageRelease{},
		},
		"error from manager": {
			mockErr: fmt.Errorf("API error"),
			expErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			mgr.On("ListReleases", mock.Anything).Return(tc.mockReleases, tc.mockErr)

			svc, err := imagelist.NewService(imagelist.ServiceConfig{Manager: mgr})
			require.NoError(t, err)

			got, err := svc.Run(context.Background())
			if tc.expErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expReleases, got)
		})
	}
}
