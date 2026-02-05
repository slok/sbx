package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvSpecs(t *testing.T) {
	t.Setenv("FROM_HOST", "host-value")

	tests := map[string]struct {
		specs  []string
		expEnv map[string]string
		expErr bool
	}{
		"KEY=VALUE should parse": {
			specs:  []string{"FOO=bar"},
			expEnv: map[string]string{"FOO": "bar"},
		},
		"KEY should inherit from host": {
			specs:  []string{"FROM_HOST"},
			expEnv: map[string]string{"FROM_HOST": "host-value"},
		},
		"Later entries should override earlier ones": {
			specs:  []string{"FOO=one", "FOO=two"},
			expEnv: map[string]string{"FOO": "two"},
		},
		"Missing inherited var should fail": {
			specs:  []string{"DOES_NOT_EXIST"},
			expErr: true,
		},
		"Invalid key should fail": {
			specs:  []string{"1INVALID=value"},
			expErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			env, err := parseEnvSpecs(tc.specs)

			if tc.expErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expEnv, env)
		})
	}
}
