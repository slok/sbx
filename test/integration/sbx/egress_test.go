package sbx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intsbx "github.com/slok/sbx/test/integration/sbx"
)

// proxyPorts mirrors firecracker.ProxyPorts for reading proxy.json in tests.
type proxyPorts struct {
	HTTPPort int `json:"http_port"`
	TLSPort  int `json:"tls_port"`
	DNSPort  int `json:"dns_port"`
}

// readProxyPorts reads proxy.json from the VM directory.
func readProxyPorts(t *testing.T, sandboxID string) proxyPorts {
	t.Helper()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	portFile := filepath.Join(home, ".sbx", "vms", sandboxID, "proxy.json")
	data, err := os.ReadFile(portFile)
	require.NoError(t, err, "could not read proxy.json at %s", portFile)

	var ports proxyPorts
	require.NoError(t, json.Unmarshal(data, &ports))
	return ports
}

// readProxyPID reads the proxy PID from proxy.pid in the VM directory.
func readProxyPID(t *testing.T, sandboxID string) int {
	t.Helper()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	pidFile := filepath.Join(home, ".sbx", "vms", sandboxID, "proxy.pid")
	data, err := os.ReadFile(pidFile)
	require.NoError(t, err, "could not read proxy.pid at %s", pidFile)

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err, "invalid PID in proxy.pid")
	return pid
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if process exists without sending a signal.
	return proc.Signal(syscall.Signal(0)) == nil
}

// getSandboxID looks up the sandbox ID by name using sbx list.
func getSandboxID(ctx context.Context, t *testing.T, config intsbx.Config, dbPath, name string) string {
	t.Helper()

	stdout, stderr, err := intsbx.RunList(ctx, config, dbPath)
	require.NoError(t, err, "list failed: stderr=%s", stderr)

	items := parseSandboxList(t, stdout)
	found := findSandboxInList(items, name)
	require.NotNil(t, found, "sandbox %s not found in list", name)
	return found.ID
}

// startSandboxWithEgress is a helper that creates and starts a sandbox with an egress session YAML.
// Returns the sandbox ID.
func startSandboxWithEgress(ctx context.Context, t *testing.T, config intsbx.Config, dbPath, name, egressYAML string) string {
	t.Helper()

	cleanupSandbox(t, config, dbPath, name)

	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	require.NoError(t, os.WriteFile(sessionFile, []byte(egressYAML), 0644))

	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	return getSandboxID(ctx, t, config, dbPath, name)
}

func TestEgressDefaultDenyBlocksTraffic(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrdeny")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sandboxID := startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-deny-test
egress:
  default: deny
`)

	// Verify proxy is running.
	ports := readProxyPorts(t, sandboxID)
	require.Greater(t, ports.HTTPPort, 0, "proxy HTTP port should be allocated")
	pid := readProxyPID(t, sandboxID)
	assert.True(t, isProcessAlive(pid), "proxy process (PID %d) should be alive", pid)

	// From inside the VM, curl to any HTTP endpoint on port 80.
	// DNAT redirects the request to the proxy, which denies it (403).
	// curl should fail with a non-zero exit code.
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "http://1.1.1.1/",
	})
	// The proxy returns 403 Forbidden. curl writes the HTTP status code to stdout.
	if err == nil {
		// curl succeeded connecting — check the response code is 403.
		assert.Equal(t, "403", strings.TrimSpace(string(stdout)),
			"expected 403 from proxy deny, got: stdout=%s stderr=%s", stdout, stderr)
	} else {
		// curl failed entirely (connection refused, etc.) — also acceptable for deny.
		t.Logf("curl failed as expected with deny policy: err=%v stderr=%s", err, stderr)
	}
}

func TestEgressDefaultAllowPassesTraffic(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrallow")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-allow-test
egress:
  default: allow
`)

	// From inside the VM, curl to a well-known HTTP endpoint on port 80.
	// DNAT redirects to the proxy, which allows it and forwards upstream.
	// The request should succeed with HTTP 200 (or 301/302 redirect, which is still a success).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "http://example.com/",
	})
	require.NoError(t, err, "curl should succeed with allow policy: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	// example.com returns 200 or a redirect (3xx) — either is a success (not 403/000).
	assert.NotEqual(t, "403", httpCode, "should not get 403 with allow policy")
	assert.NotEqual(t, "000", httpCode, "should not get connection failure with allow policy")
}

func TestEgressAllowRuleOverridesDenyDefault(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrover")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-rule-override-test
egress:
  default: deny
  rules:
    - domain: "*"
      action: allow
`)

	// Wildcard allow rule overrides deny default.
	// From inside the VM, curl should succeed.
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "http://example.com/",
	})
	require.NoError(t, err, "curl should succeed with wildcard allow rule: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "403", httpCode, "should not get 403 with wildcard allow rule")
	assert.NotEqual(t, "000", httpCode, "should not get connection failure with wildcard allow rule")
}

func TestEgressHTTPSAllowPassesTraffic(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrtls")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sandboxID := startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-tls-allow-test
egress:
  default: allow
`)

	// Verify TLS proxy port is allocated.
	ports := readProxyPorts(t, sandboxID)
	require.Greater(t, ports.TLSPort, 0, "TLS proxy port should be allocated")

	// From inside the VM, curl an HTTPS endpoint.
	// DNAT redirects port 443 to the transparent TLS proxy, which reads the SNI,
	// allows it, and tunnels the TLS handshake to the real server.
	// We use httpbin.org instead of example.com because example.com's Cloudflare
	// certificate chain is not fully trusted by the Alpine CA bundle.
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "https://httpbin.org/get",
	})
	require.NoError(t, err, "curl https should succeed with allow policy: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "000", httpCode, "should not get connection failure for HTTPS with allow policy")
}

func TestEgressHTTPSDenyBlocksTraffic(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrtlsd")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-tls-deny-test
egress:
  default: deny
`)

	// From inside the VM, curl an HTTPS endpoint.
	// DNAT redirects port 443 to the transparent TLS proxy, which reads the SNI,
	// denies it, and closes the connection. curl should fail.
	_, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "https://httpbin.org/get",
	})
	assert.Error(t, err, "curl https should fail with deny policy: stderr=%s", stderr)
}

func TestEgressHTTPSAllowRuleOverridesDeny(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrtlsr")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-tls-rule-test
egress:
  default: deny
  rules:
    - domain: "httpbin.org"
      action: allow
`)

	// From inside the VM, curl to httpbin.org over HTTPS — allowed by rule.
	// DNS must also resolve (DNS proxy also checks rules, "httpbin.org" matches the allow rule).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "https://httpbin.org/get",
	})
	require.NoError(t, err, "curl https should succeed with allow rule for httpbin.org: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "000", httpCode, "should not get connection failure for HTTPS with allow rule")
}

func TestEgressProxyKilledOnStop(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrstop")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sandboxID := startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-stop-test
egress:
  default: allow
`)

	// Verify proxy is alive.
	pid := readProxyPID(t, sandboxID)
	require.True(t, isProcessAlive(pid), "proxy process should be alive while sandbox is running")

	// Stop sandbox.
	_, stderr, err := intsbx.RunStop(ctx, config, dbPath, name)
	require.NoError(t, err, "stop failed: stderr=%s", stderr)

	// Wait briefly for cleanup.
	time.Sleep(500 * time.Millisecond)

	// Proxy should be dead.
	assert.False(t, isProcessAlive(pid), "proxy process (PID %d) should be killed after stop", pid)
}

func TestEgressNonStandardPortBlocked(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrport")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-port-block-test
egress:
  default: allow
`)

	// Standard ports should work (DNAT'd to proxy, allowed by default policy).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "http://example.com/",
	})
	require.NoError(t, err, "HTTP on port 80 should work: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "000", httpCode, "HTTP on port 80 should not get connection failure")

	// Non-standard port should be blocked by forward-egress chain.
	// nc to a well-known IP on port 853 (DNS-over-TLS) should fail.
	_, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{
		"bash", "-c", "echo test | nc -w 3 1.1.1.1 853",
	})
	assert.Error(t, err, "connection to port 853 should be blocked by forward-egress: stderr=%s", stderr)
}

func TestEgressProxyBoundToGateway(t *testing.T) {
	// Verify that the proxy is bound to the gateway IP only, not all interfaces.
	// After the bind-address fix, the proxy should not be reachable from inside the VM
	// on any IP other than the gateway (10.68.X.1). This test checks that existing
	// egress functionality works correctly with the proxy bound to the gateway IP
	// (regression test for the bind-address change).
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrbind")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sandboxID := startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-bind-test
egress:
  default: allow
`)

	// Verify proxy is running and ports are allocated.
	ports := readProxyPorts(t, sandboxID)
	require.Greater(t, ports.HTTPPort, 0, "proxy HTTP port should be allocated")
	pid := readProxyPID(t, sandboxID)
	assert.True(t, isProcessAlive(pid), "proxy process (PID %d) should be alive", pid)

	// HTTP traffic should still work through DNAT (regression test).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "http://example.com/",
	})
	require.NoError(t, err, "HTTP via DNAT should still work with bind-address: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "000", httpCode, "HTTP should not fail with proxy bound to gateway")

	// HTTPS traffic should also still work through DNAT.
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "https://httpbin.org/get",
	})
	require.NoError(t, err, "HTTPS via DNAT should still work with bind-address: stderr=%s", stderr)
	httpCode = strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "000", httpCode, "HTTPS should not fail with proxy bound to gateway")
}

func TestEgressInputChainBlocksDirectProxyAccess(t *testing.T) {
	// The input-egress nftables chain should block the VM from connecting directly
	// to the proxy's actual listening ports on the gateway IP. Without this chain,
	// an attacker inside the VM could port-scan the gateway, find the proxy ports,
	// and use the HTTP proxy's CONNECT method to tunnel to any destination.
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrinp")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	sandboxID := startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-input-test
egress:
  default: allow
  rules:
    - { action: deny, domain: "github.com" }
    - { action: deny, domain: "*.github.com" }
`)

	ports := readProxyPorts(t, sandboxID)

	// Normal egress should still work (DNAT'd traffic is accepted by input-egress).
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "10", "http://example.com/",
	})
	require.NoError(t, err, "HTTP via DNAT should work: stderr=%s", stderr)
	httpCode := strings.TrimSpace(string(stdout))
	assert.NotEqual(t, "000", httpCode, "HTTP via DNAT should succeed")

	// Direct connection to the proxy HTTP port on the gateway should be blocked.
	// The VM tries to connect to gateway:proxy-http-port directly (not via DNAT).
	// The input-egress chain should drop this traffic.
	_, _, err = intsbx.RunExec(ctx, config, dbPath, name, []string{
		"bash", "-c", fmt.Sprintf("echo test | nc -w 3 10.68.40.1 %d", ports.HTTPPort),
	})
	assert.Error(t, err, "direct connection to proxy HTTP port (%d) should be blocked by input-egress", ports.HTTPPort)

	// Direct connection to the proxy TLS port should also be blocked.
	_, _, err = intsbx.RunExec(ctx, config, dbPath, name, []string{
		"bash", "-c", fmt.Sprintf("echo test | nc -w 3 10.68.40.1 %d", ports.TLSPort),
	})
	assert.Error(t, err, "direct connection to proxy TLS port (%d) should be blocked by input-egress", ports.TLSPort)

	// Direct connection to the proxy DNS port should also be blocked.
	_, _, err = intsbx.RunExec(ctx, config, dbPath, name, []string{
		"bash", "-c", fmt.Sprintf("echo test | nc -w 3 10.68.40.1 %d", ports.DNSPort),
	})
	assert.Error(t, err, "direct connection to proxy DNS port (%d) should be blocked by input-egress", ports.DNSPort)

	// Verify that the previous bypass (using proxy directly with -x flag) no longer works.
	stdout, stderr, err = intsbx.RunExec(ctx, config, dbPath, name, []string{
		"curl", "-x", fmt.Sprintf("http://10.68.40.1:%d", ports.HTTPPort),
		"-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "http://github.com/",
	})
	if err == nil {
		// If curl managed to connect, verify it did NOT get a successful proxy response.
		assert.Equal(t, "000", strings.TrimSpace(string(stdout)),
			"direct proxy bypass should not succeed: stderr=%s", stderr)
	}
	// err != nil is the expected case: connection dropped by input-egress.
}

func TestEgressInputChainBlocksHostServices(t *testing.T) {
	// The input-egress chain should prevent the VM from reaching arbitrary host
	// services on the gateway IP. Without this, the VM could access host-local
	// services like Ollama (11434), dev servers, databases, etc.
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrhost")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_ = startSandboxWithEgress(ctx, t, config, dbPath, name, `name: egress-host-block-test
egress:
  default: allow
`)

	// Try to connect to common host service ports on the gateway.
	// All should be blocked by the input-egress chain.
	blockedPorts := []struct {
		port int
		name string
	}{
		{11434, "Ollama"},
		{3000, "dev-server"},
		{8080, "alt-HTTP"},
		{22, "SSH"},
	}

	for _, bp := range blockedPorts {
		_, _, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
			"bash", "-c", fmt.Sprintf("echo test | nc -w 3 10.68.40.1 %d", bp.port),
		})
		assert.Error(t, err, "connection to gateway port %d (%s) should be blocked by input-egress", bp.port, bp.name)
	}
}

func TestEgressNoProxyWithoutEgressConfig(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrno")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Start without egress — no session file, regular start.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStart(ctx, config, dbPath, name)
	require.NoError(t, err, "start failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Get sandbox ID and verify no proxy.json exists.
	sandboxID := getSandboxID(ctx, t, config, dbPath, name)

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	portFile := filepath.Join(home, ".sbx", "vms", sandboxID, "proxy.json")
	_, err = os.Stat(portFile)
	assert.True(t, os.IsNotExist(err), "proxy.json should not exist when no egress config is set")

	pidFile := filepath.Join(home, ".sbx", "vms", sandboxID, "proxy.pid")
	_, err = os.Stat(pidFile)
	assert.True(t, os.IsNotExist(err), "proxy.pid should not exist when no egress config is set")
}
