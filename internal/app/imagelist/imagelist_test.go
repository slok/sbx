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
		mockLocal   func(m *imagemock.MockImageManager)
		mockPuller  func(m *imagemock.MockImagePuller)
		usePuller   bool
		expReleases []model.ImageRelease
		expErr      bool
	}{
		"Listing local images without puller should return only local images.": {
			mockLocal: func(m *imagemock.MockImageManager) {
				m.On("List", mock.Anything).Return([]model.ImageRelease{
					{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
					{Version: "my-snap", Installed: true, Source: model.ImageSourceSnapshot},
				}, nil)
			},
			expReleases: []model.ImageRelease{
				{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
				{Version: "my-snap", Installed: true, Source: model.ImageSourceSnapshot},
			},
		},

		"Listing with puller should merge local and remote images.": {
			usePuller: true,
			mockLocal: func(m *imagemock.MockImageManager) {
				m.On("List", mock.Anything).Return([]model.ImageRelease{
					{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
				}, nil)
			},
			mockPuller: func(m *imagemock.MockImagePuller) {
				m.On("ListRemote", mock.Anything).Return([]model.ImageRelease{
					{Version: "v0.2.0", Source: model.ImageSourceRelease},
					{Version: "v0.1.0", Source: model.ImageSourceRelease},
				}, nil)
			},
			expReleases: []model.ImageRelease{
				{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
				{Version: "v0.2.0", Source: model.ImageSourceRelease},
			},
		},

		"An error from the local image manager should propagate.": {
			mockLocal: func(m *imagemock.MockImageManager) {
				m.On("List", mock.Anything).Return(nil, fmt.Errorf("disk error"))
			},
			expErr: true,
		},

		"An error from the puller should propagate.": {
			usePuller: true,
			mockLocal: func(m *imagemock.MockImageManager) {
				m.On("List", mock.Anything).Return([]model.ImageRelease{}, nil)
			},
			mockPuller: func(m *imagemock.MockImagePuller) {
				m.On("ListRemote", mock.Anything).Return(nil, fmt.Errorf("API error"))
			},
			expErr: true,
		},

		"Listing with empty local and empty remote should return empty.": {
			usePuller: true,
			mockLocal: func(m *imagemock.MockImageManager) {
				m.On("List", mock.Anything).Return([]model.ImageRelease{}, nil)
			},
			mockPuller: func(m *imagemock.MockImagePuller) {
				m.On("ListRemote", mock.Anything).Return([]model.ImageRelease{}, nil)
			},
			expReleases: []model.ImageRelease{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			if tc.mockLocal != nil {
				tc.mockLocal(mgr)
			}

			cfg := imagelist.ServiceConfig{Manager: mgr}

			if tc.usePuller {
				puller := imagemock.NewMockImagePuller(t)
				if tc.mockPuller != nil {
					tc.mockPuller(puller)
				}
				cfg.Puller = puller
			}

			svc, err := imagelist.NewService(cfg)
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
