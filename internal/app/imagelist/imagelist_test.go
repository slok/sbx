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
		mockReleases  []model.ImageRelease
		mockErr       error
		mockSnapshots []model.ImageRelease
		mockSnapErr   error
		useSnapMgr    bool
		expReleases   []model.ImageRelease
		expErr        bool
	}{
		"Listing releases without snapshot manager should return only releases.": {
			mockReleases: []model.ImageRelease{
				{Version: "v0.2.0", Installed: false, Source: model.ImageSourceRelease},
				{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
			},
			expReleases: []model.ImageRelease{
				{Version: "v0.2.0", Installed: false, Source: model.ImageSourceRelease},
				{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
			},
		},

		"Listing with empty releases should return empty.": {
			mockReleases: []model.ImageRelease{},
			expReleases:  []model.ImageRelease{},
		},

		"An error from the image manager should propagate.": {
			mockErr: fmt.Errorf("API error"),
			expErr:  true,
		},

		"Listing with snapshot manager should merge releases and snapshots.": {
			useSnapMgr: true,
			mockReleases: []model.ImageRelease{
				{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
			},
			mockSnapshots: []model.ImageRelease{
				{Version: "my-snap", Installed: true, Source: model.ImageSourceSnapshot},
			},
			expReleases: []model.ImageRelease{
				{Version: "v0.1.0", Installed: true, Source: model.ImageSourceRelease},
				{Version: "my-snap", Installed: true, Source: model.ImageSourceSnapshot},
			},
		},

		"An error from the snapshot manager should propagate.": {
			useSnapMgr:   true,
			mockReleases: []model.ImageRelease{},
			mockSnapErr:  fmt.Errorf("disk error"),
			expErr:       true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			mgr.On("ListReleases", mock.Anything).Return(tc.mockReleases, tc.mockErr)

			cfg := imagelist.ServiceConfig{Manager: mgr}

			if tc.useSnapMgr {
				snapMgr := imagemock.NewMockSnapshotManager(t)
				snapMgr.On("List", mock.Anything).Return(tc.mockSnapshots, tc.mockSnapErr)
				cfg.SnapshotManager = snapMgr
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
