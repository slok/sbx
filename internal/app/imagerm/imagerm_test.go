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
		version string
		mockErr error
		expErr  bool
	}{
		"Removing an installed image should succeed.": {
			version: "v0.1.0",
		},

		"Removing a snapshot image should succeed.": {
			version: "my-snap",
		},

		"An error from the image manager should propagate.": {
			version: "v0.1.0",
			mockErr: fmt.Errorf("not installed"),
			expErr:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := imagemock.NewMockImageManager(t)
			mgr.On("Remove", mock.Anything, tc.version).Return(tc.mockErr)

			svc, err := imagerm.NewService(imagerm.ServiceConfig{Manager: mgr})
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
