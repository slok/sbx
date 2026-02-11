package egress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
)

func TestNewProxy(t *testing.T) {
	tests := map[string]struct {
		cfg    ProxyConfig
		expErr bool
	}{
		"Valid config should not error.": {
			cfg: ProxyConfig{
				ListenAddr: "127.0.0.1:8443",
				Policy: model.EgressPolicy{
					Default: model.EgressActionDeny,
					Rules: []model.EgressRule{
						{Domain: "github.com", Action: model.EgressActionAllow},
					},
				},
			},
		},
		"Missing listen address should error.": {
			cfg: ProxyConfig{
				Policy: model.EgressPolicy{Default: model.EgressActionDeny},
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := NewProxy(test.cfg)
			if test.expErr {
				assert.Error(t, err)
				assert.Nil(t, p)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, p)
			}
		})
	}
}

func TestProxyClassificationAndPolicy(t *testing.T) {
	// Test the classification + policy flow without actual network connections.
	// This tests that the Classify + PolicyMatcher integration works correctly.
	policy := model.EgressPolicy{
		Default: model.EgressActionDeny,
		Rules: []model.EgressRule{
			{Domain: "github.com", Action: model.EgressActionAllow},
			{Domain: "*.npmjs.org", Action: model.EgressActionAllow},
			{CIDR: "10.0.0.0/8", Action: model.EgressActionAllow},
		},
	}
	matcher := NewPolicyMatcher(policy)

	tests := map[string]struct {
		input func(t *testing.T) []byte
		expOk bool
	}{
		"TLS to allowed domain should be allowed.": {
			input: func(t *testing.T) []byte {
				return captureTLSClientHello(t, "github.com")
			},
			expOk: true,
		},
		"TLS to denied domain should be denied.": {
			input: func(t *testing.T) []byte {
				return captureTLSClientHello(t, "evil.com")
			},
			expOk: false,
		},
		"TLS to wildcard-allowed subdomain should be allowed.": {
			input: func(t *testing.T) []byte {
				return captureTLSClientHello(t, "registry.npmjs.org")
			},
			expOk: true,
		},
		"HTTP to allowed domain should be allowed.": {
			input: func(t *testing.T) []byte {
				return []byte("GET / HTTP/1.1\r\nHost: github.com\r\n\r\n")
			},
			expOk: true,
		},
		"HTTP to denied domain should be denied.": {
			input: func(t *testing.T) []byte {
				return []byte("GET / HTTP/1.1\r\nHost: evil.com\r\n\r\n")
			},
			expOk: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			buf := test.input(t)
			result := Classify(buf)

			var allowed bool
			if result.Host != "" {
				allowed = matcher.AllowDomain(result.Host)
			} else {
				allowed = matcher.AllowIP(nil)
			}

			assert.Equal(t, test.expOk, allowed)
		})
	}
}
