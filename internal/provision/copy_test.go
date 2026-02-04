package provision_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/provision"
	"github.com/slok/sbx/internal/provision/provisionmock"
)

func TestNewCopyPath(t *testing.T) {
	tests := map[string]struct {
		cfg    provision.CopyPathConfig
		expErr bool
	}{
		"Missing accessor should fail.": {
			cfg: provision.CopyPathConfig{
				SrcLocal:  "/local/file",
				DstRemote: "/remote/file",
			},
			expErr: true,
		},

		"Missing src local should fail.": {
			cfg: provision.CopyPathConfig{
				Accessor:  &provisionmock.MockSandboxAccessor{},
				DstRemote: "/remote/file",
			},
			expErr: true,
		},

		"Missing dst remote should fail.": {
			cfg: provision.CopyPathConfig{
				Accessor: &provisionmock.MockSandboxAccessor{},
				SrcLocal: "/local/file",
			},
			expErr: true,
		},

		"Valid config should succeed.": {
			cfg: provision.CopyPathConfig{
				Accessor:  &provisionmock.MockSandboxAccessor{},
				SrcLocal:  "/local/file",
				DstRemote: "/remote/file",
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			_, err := provision.NewCopyPath(test.cfg)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestCopyPathProvision(t *testing.T) {
	tests := map[string]struct {
		mock      func(m *provisionmock.MockSandboxAccessor)
		srcLocal  string
		dstRemote string
		expErr    bool
	}{
		"Copying a path successfully should not return error.": {
			srcLocal:  "/host/myfile.txt",
			dstRemote: "/sandbox/myfile.txt",
			mock: func(m *provisionmock.MockSandboxAccessor) {
				m.On("CopyTo", mock.Anything, "/host/myfile.txt", "/sandbox/myfile.txt").Once().Return(nil)
			},
			expErr: false,
		},

		"Copying a directory successfully should not return error.": {
			srcLocal:  "/host/mydir",
			dstRemote: "/sandbox/mydir",
			mock: func(m *provisionmock.MockSandboxAccessor) {
				m.On("CopyTo", mock.Anything, "/host/mydir", "/sandbox/mydir").Once().Return(nil)
			},
			expErr: false,
		},

		"An error from the accessor should be propagated.": {
			srcLocal:  "/host/file",
			dstRemote: "/sandbox/file",
			mock: func(m *provisionmock.MockSandboxAccessor) {
				m.On("CopyTo", mock.Anything, "/host/file", "/sandbox/file").Once().Return(fmt.Errorf("copy failed"))
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mAccessor := &provisionmock.MockSandboxAccessor{}
			test.mock(mAccessor)

			p, err := provision.NewCopyPath(provision.CopyPathConfig{
				Accessor:  mAccessor,
				SrcLocal:  test.srcLocal,
				DstRemote: test.dstRemote,
				Logger:    log.Noop,
			})
			require.NoError(err)

			err = p.Provision(context.TODO())

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}

			mAccessor.AssertExpectations(t)
		})
	}
}
