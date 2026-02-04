package provision_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/provision"
	"github.com/slok/sbx/internal/provision/provisionmock"
	"github.com/slok/sbx/internal/sandbox/sandboxmock"
)

func TestProvisionerChain(t *testing.T) {
	tests := map[string]struct {
		mock   func(provisioners []*provisionmock.MockProvisioner)
		count  int
		expErr bool
	}{
		"An empty chain should succeed immediately.": {
			count:  0,
			mock:   func(provisioners []*provisionmock.MockProvisioner) {},
			expErr: false,
		},

		"A single provisioner that succeeds should succeed.": {
			count: 1,
			mock: func(provisioners []*provisionmock.MockProvisioner) {
				provisioners[0].On("Provision", mock.Anything).Once().Return(nil)
			},
			expErr: false,
		},

		"Multiple provisioners that all succeed should succeed.": {
			count: 3,
			mock: func(provisioners []*provisionmock.MockProvisioner) {
				provisioners[0].On("Provision", mock.Anything).Once().Return(nil)
				provisioners[1].On("Provision", mock.Anything).Once().Return(nil)
				provisioners[2].On("Provision", mock.Anything).Once().Return(nil)
			},
			expErr: false,
		},

		"A chain should stop and return error when a provisioner fails.": {
			count: 3,
			mock: func(provisioners []*provisionmock.MockProvisioner) {
				provisioners[0].On("Provision", mock.Anything).Once().Return(nil)
				provisioners[1].On("Provision", mock.Anything).Once().Return(fmt.Errorf("something broke"))
				// provisioners[2] should NOT be called.
			},
			expErr: true,
		},

		"A chain should stop if context is cancelled before a provisioner runs.": {
			count: 2,
			mock: func(provisioners []*provisionmock.MockProvisioner) {
				// With a pre-cancelled context, no provisioners should be called.
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// Special case: test cancelled context.
			ctx := context.TODO()
			if name == "A chain should stop if context is cancelled before a provisioner runs." {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel() // Cancel immediately.
			}

			// Create mock provisioners.
			mocks := make([]*provisionmock.MockProvisioner, test.count)
			provisioners := make([]provision.Provisioner, test.count)
			for i := 0; i < test.count; i++ {
				m := &provisionmock.MockProvisioner{}
				mocks[i] = m
				provisioners[i] = m
			}

			test.mock(mocks)

			chain := provision.NewProvisionerChain(provisioners...)
			err := chain.Provision(ctx)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}

			for _, m := range mocks {
				m.AssertExpectations(t)
			}
		})
	}
}

func TestProvisionerFunc(t *testing.T) {
	tests := map[string]struct {
		fn     provision.ProvisionerFunc
		expErr bool
	}{
		"A ProvisionerFunc that returns nil should succeed.": {
			fn:     func(_ context.Context) error { return nil },
			expErr: false,
		},

		"A ProvisionerFunc that returns an error should fail.": {
			fn:     func(_ context.Context) error { return fmt.Errorf("test error") },
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			err := test.fn.Provision(context.TODO())

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestNoopProvisioner(t *testing.T) {
	assert := assert.New(t)

	p := provision.NewNoopProvisioner()
	err := p.Provision(context.TODO())
	assert.NoError(err)
}

func TestLogProvisioner(t *testing.T) {
	tests := map[string]struct {
		mock   func(m *provisionmock.MockProvisioner)
		expErr bool
	}{
		"A LogProvisioner wrapping a successful provisioner should succeed.": {
			mock: func(m *provisionmock.MockProvisioner) {
				m.On("Provision", mock.Anything).Once().Return(nil)
			},
			expErr: false,
		},

		"A LogProvisioner wrapping a failing provisioner should propagate the error.": {
			mock: func(m *provisionmock.MockProvisioner) {
				m.On("Provision", mock.Anything).Once().Return(fmt.Errorf("inner error"))
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mp := &provisionmock.MockProvisioner{}
			test.mock(mp)

			p := provision.NewLogProvisioner("test-provisioner", log.Noop, mp)
			err := p.Provision(context.TODO())

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}

			mp.AssertExpectations(t)
		})
	}
}

func TestSandboxAccessor(t *testing.T) {
	tests := map[string]struct {
		mock      func(m *sandboxmock.MockEngine)
		test      func(assert *assert.Assertions, require *require.Assertions, accessor provision.SandboxAccessor)
		sandboxID string
	}{
		"Exec should delegate to engine with the bound sandbox ID.": {
			sandboxID: "sb-123",
			mock: func(m *sandboxmock.MockEngine) {
				m.On("Exec", mock.Anything, "sb-123", []string{"echo", "hello"}, mock.AnythingOfType("model.ExecOpts")).
					Once().Return(&model.ExecResult{ExitCode: 0}, nil)
			},
			test: func(assert *assert.Assertions, require *require.Assertions, accessor provision.SandboxAccessor) {
				result, err := accessor.Exec(context.TODO(), []string{"echo", "hello"}, model.ExecOpts{})
				require.NoError(err)
				assert.Equal(0, result.ExitCode)
			},
		},

		"CopyTo should delegate to engine with the bound sandbox ID.": {
			sandboxID: "sb-456",
			mock: func(m *sandboxmock.MockEngine) {
				m.On("CopyTo", mock.Anything, "sb-456", "/local/file.txt", "/remote/file.txt").
					Once().Return(nil)
			},
			test: func(assert *assert.Assertions, require *require.Assertions, accessor provision.SandboxAccessor) {
				err := accessor.CopyTo(context.TODO(), "/local/file.txt", "/remote/file.txt")
				require.NoError(err)
			},
		},

		"Exec should propagate engine errors.": {
			sandboxID: "sb-789",
			mock: func(m *sandboxmock.MockEngine) {
				m.On("Exec", mock.Anything, "sb-789", []string{"fail"}, mock.AnythingOfType("model.ExecOpts")).
					Once().Return((*model.ExecResult)(nil), fmt.Errorf("exec failed"))
			},
			test: func(assert *assert.Assertions, _ *require.Assertions, accessor provision.SandboxAccessor) {
				_, err := accessor.Exec(context.TODO(), []string{"fail"}, model.ExecOpts{})
				assert.Error(err)
			},
		},

		"CopyTo should propagate engine errors.": {
			sandboxID: "sb-000",
			mock: func(m *sandboxmock.MockEngine) {
				m.On("CopyTo", mock.Anything, "sb-000", "/src", "/dst").
					Once().Return(fmt.Errorf("copy failed"))
			},
			test: func(assert *assert.Assertions, _ *require.Assertions, accessor provision.SandboxAccessor) {
				err := accessor.CopyTo(context.TODO(), "/src", "/dst")
				assert.Error(err)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			mEngine := &sandboxmock.MockEngine{}
			test.mock(mEngine)

			accessor := provision.NewSandboxAccessor(mEngine, test.sandboxID)
			test.test(assert, require, accessor)

			mEngine.AssertExpectations(t)
		})
	}
}
