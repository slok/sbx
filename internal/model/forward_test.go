package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
)

func TestParsePortMapping(t *testing.T) {
	tests := map[string]struct {
		input    string
		expected model.PortMapping
		expErr   bool
	}{
		"Short form single port should use same local and remote.": {
			input:    "8080",
			expected: model.PortMapping{LocalPort: 8080, RemotePort: 8080},
		},
		"Full form with same ports should parse correctly.": {
			input:    "8080:8080",
			expected: model.PortMapping{LocalPort: 8080, RemotePort: 8080},
		},
		"Full form with different ports should parse correctly.": {
			input:    "9000:8080",
			expected: model.PortMapping{LocalPort: 9000, RemotePort: 8080},
		},
		"Minimum valid port should work.": {
			input:    "1",
			expected: model.PortMapping{LocalPort: 1, RemotePort: 1},
		},
		"Maximum valid port should work.": {
			input:    "65535",
			expected: model.PortMapping{LocalPort: 65535, RemotePort: 65535},
		},
		"Port with whitespace should be trimmed.": {
			input:    "  8080  ",
			expected: model.PortMapping{LocalPort: 8080, RemotePort: 8080},
		},
		"Full form with whitespace should be trimmed.": {
			input:    " 9000 : 8080 ",
			expected: model.PortMapping{LocalPort: 9000, RemotePort: 8080},
		},
		"Empty string should fail.": {
			input:  "",
			expErr: true,
		},
		"Only whitespace should fail.": {
			input:  "   ",
			expErr: true,
		},
		"Port zero should fail.": {
			input:  "0",
			expErr: true,
		},
		"Negative port should fail.": {
			input:  "-1",
			expErr: true,
		},
		"Port above maximum should fail.": {
			input:  "65536",
			expErr: true,
		},
		"Non-numeric port should fail.": {
			input:  "abc",
			expErr: true,
		},
		"Too many colons should fail.": {
			input:  "8080:8080:8080",
			expErr: true,
		},
		"Invalid local port in full form should fail.": {
			input:  "abc:8080",
			expErr: true,
		},
		"Invalid remote port in full form should fail.": {
			input:  "8080:abc",
			expErr: true,
		},
		"Zero local port in full form should fail.": {
			input:  "0:8080",
			expErr: true,
		},
		"Zero remote port in full form should fail.": {
			input:  "8080:0",
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			result, err := model.ParsePortMapping(test.input)

			if test.expErr {
				assert.Error(err)
			} else {
				require.NoError(err)
				assert.Equal(test.expected, result)
			}
		})
	}
}

func TestPortMappingString(t *testing.T) {
	tests := map[string]struct {
		pm       model.PortMapping
		expected string
	}{
		"Same local and remote should return short form.": {
			pm:       model.PortMapping{LocalPort: 8080, RemotePort: 8080},
			expected: "8080",
		},
		"Different local and remote should return full form.": {
			pm:       model.PortMapping{LocalPort: 9000, RemotePort: 8080},
			expected: "9000:8080",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			assert.Equal(test.expected, test.pm.String())
		})
	}
}

func TestPortMappingListenAddress(t *testing.T) {
	tests := map[string]struct {
		pm       model.PortMapping
		expected string
	}{
		"Empty bind address should default to localhost.": {
			pm:       model.PortMapping{LocalPort: 8080, RemotePort: 8080},
			expected: "localhost",
		},
		"Explicit localhost should return localhost.": {
			pm:       model.PortMapping{BindAddress: "localhost", LocalPort: 8080, RemotePort: 8080},
			expected: "localhost",
		},
		"0.0.0.0 should return 0.0.0.0.": {
			pm:       model.PortMapping{BindAddress: "0.0.0.0", LocalPort: 8080, RemotePort: 8080},
			expected: "0.0.0.0",
		},
		"Custom address should be returned as-is.": {
			pm:       model.PortMapping{BindAddress: "192.168.1.100", LocalPort: 3000, RemotePort: 3000},
			expected: "192.168.1.100",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			assert.Equal(test.expected, test.pm.ListenAddress())
		})
	}
}
