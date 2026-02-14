package firecracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/conventions"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

func TestBuildProxyArgs(t *testing.T) {
	tests := map[string]struct {
		egress   model.EgressPolicy
		httpPort int
		tlsPort  int
		dnsPort  int
		expArgs  []string
	}{
		"Allow-default policy with no rules.": {
			egress:   model.EgressPolicy{Default: model.EgressActionAllow},
			httpPort: 8080,
			tlsPort:  8443,
			dnsPort:  5353,
			expArgs: []string{
				"--no-log",
				"internal-vm-proxy",
				"--port", "8080",
				"--tls-port", "8443",
				"--dns-port", "5353",
				"--default-policy", "allow",
			},
		},

		"Deny-default policy with rules.": {
			egress: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Action: model.EgressActionAllow, Domain: "github.com"},
					{Action: model.EgressActionAllow, Domain: "*.github.com"},
				},
			},
			httpPort: 9090,
			tlsPort:  9443,
			dnsPort:  5354,
			expArgs: []string{
				"--no-log",
				"internal-vm-proxy",
				"--port", "9090",
				"--tls-port", "9443",
				"--dns-port", "5354",
				"--default-policy", "deny",
				"--rule", `{"action":"allow","domain":"github.com"}`,
				"--rule", `{"action":"allow","domain":"*.github.com"}`,
			},
		},

		"Allow-default policy with deny rule.": {
			egress: model.EgressPolicy{
				Default: model.EgressActionAllow,
				Rules: []model.EgressRule{
					{Action: model.EgressActionDeny, Domain: "evil.com"},
				},
			},
			httpPort: 3128,
			tlsPort:  3129,
			dnsPort:  5300,
			expArgs: []string{
				"--no-log",
				"internal-vm-proxy",
				"--port", "3128",
				"--tls-port", "3129",
				"--dns-port", "5300",
				"--default-policy", "allow",
				"--rule", `{"action":"deny","domain":"evil.com"}`,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			got := buildProxyArgs(test.egress, test.httpPort, test.tlsPort, test.dnsPort)
			assert.Equal(test.expArgs, got)
		})
	}
}

func TestKillProxy_NoPIDFile(t *testing.T) {
	e := &Engine{logger: log.Noop}
	vmDir := t.TempDir()

	err := e.killProxy(vmDir)
	assert.NoError(t, err)
}

func TestKillProxy_InvalidPID(t *testing.T) {
	e := &Engine{logger: log.Noop}
	vmDir := t.TempDir()

	pidPath := filepath.Join(vmDir, conventions.ProxyPIDFile)
	err := os.WriteFile(pidPath, []byte("not-a-number"), 0644)
	require.NoError(t, err)

	err = e.killProxy(vmDir)
	assert.Error(t, err)
}

func TestKillProxy_ProcessNotExist(t *testing.T) {
	e := &Engine{logger: log.Noop}
	vmDir := t.TempDir()

	pidPath := filepath.Join(vmDir, conventions.ProxyPIDFile)
	err := os.WriteFile(pidPath, []byte("999999"), 0644)
	require.NoError(t, err)

	// Should handle gracefully — process doesn't exist.
	err = e.killProxy(vmDir)
	assert.NoError(t, err)
}

func TestReadProxyPorts(t *testing.T) {
	tests := map[string]struct {
		setup    func(t *testing.T, vmDir string)
		expPorts ProxyPorts
		expErr   bool
	}{
		"Valid port file.": {
			setup: func(t *testing.T, vmDir string) {
				ports := ProxyPorts{HTTPPort: 8080, TLSPort: 8443, DNSPort: 5353}
				data, err := json.Marshal(ports)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(vmDir, conventions.ProxyPortFile), data, 0644)
				require.NoError(t, err)
			},
			expPorts: ProxyPorts{HTTPPort: 8080, TLSPort: 8443, DNSPort: 5353},
		},

		"Missing port file.": {
			setup:  func(t *testing.T, vmDir string) {},
			expErr: true,
		},

		"Invalid JSON.": {
			setup: func(t *testing.T, vmDir string) {
				err := os.WriteFile(filepath.Join(vmDir, conventions.ProxyPortFile), []byte("not-json"), 0644)
				require.NoError(t, err)
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			vmDir := t.TempDir()
			test.setup(t, vmDir)

			ports, err := readProxyPorts(vmDir)
			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.Equal(test.expPorts, ports)
			}
		})
	}
}

func TestGetFreePort(t *testing.T) {
	port, err := getFreePort()
	require.NoError(t, err)
	assert.Greater(t, port, 0)
	assert.LessOrEqual(t, port, 65535)
}

func TestGetFreeUDPPort(t *testing.T) {
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	assert.Greater(t, port, 0)
	assert.LessOrEqual(t, port, 65535)
}
