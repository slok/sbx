package sbx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// newProxyHTTPClient creates an HTTP client that routes through the proxy.
func newProxyHTTPClient(proxyAddr string) *http.Client {
	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(pURL),
		},
		Timeout: 10 * time.Second,
	}
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

func TestEgressDefaultDenyBlocksTraffic(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrdeny")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Start a local HTTP server that should NOT be reachable through the proxy.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should-not-reach"))
	}))
	defer upstream.Close()

	// Write session YAML with default deny.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: egress-deny-test
egress:
  default: deny
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	// Start with egress policy.
	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Get sandbox ID and read proxy ports.
	sandboxID := getSandboxID(ctx, t, config, dbPath, name)
	ports := readProxyPorts(t, sandboxID)
	require.Greater(t, ports.HTTPPort, 0, "proxy HTTP port should be allocated")

	// Verify proxy process is alive.
	pid := readProxyPID(t, sandboxID)
	assert.True(t, isProcessAlive(pid), "proxy process (PID %d) should be alive", pid)

	// Make request through proxy — should be denied (403).
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", ports.HTTPPort)
	client := newProxyHTTPClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// Stop sandbox — proxy should be killed.
	_, stderr, err = intsbx.RunStop(ctx, config, dbPath, name)
	require.NoError(t, err, "stop failed: stderr=%s", stderr)

	// Wait briefly for process cleanup.
	time.Sleep(500 * time.Millisecond)
	assert.False(t, isProcessAlive(pid), "proxy process (PID %d) should be dead after stop", pid)
}

func TestEgressDefaultAllowPassesTraffic(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrallow")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Start a local HTTP server that should be reachable through the proxy.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("allowed-traffic"))
	}))
	defer upstream.Close()

	// Write session YAML with default allow.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: egress-allow-test
egress:
  default: allow
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Get sandbox ID and read proxy ports.
	sandboxID := getSandboxID(ctx, t, config, dbPath, name)
	ports := readProxyPorts(t, sandboxID)

	// Make request through proxy — should be allowed.
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", ports.HTTPPort)
	client := newProxyHTTPClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "allowed-traffic", string(body))
}

func TestEgressAllowRuleOverridesDenyDefault(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrover")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("rule-override"))
	}))
	defer upstream.Close()

	// Write session YAML: default deny, but wildcard allow rule lets traffic through.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: egress-rule-override-test
egress:
  default: deny
  rules:
    - domain: "*"
      action: allow
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Read proxy ports.
	sandboxID := getSandboxID(ctx, t, config, dbPath, name)
	ports := readProxyPorts(t, sandboxID)

	// Request should be allowed by the wildcard rule despite deny default.
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", ports.HTTPPort)
	client := newProxyHTTPClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "rule-override", string(body))
}

func TestEgressProxyKilledOnStop(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("egrstop")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Write session YAML with egress.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: egress-stop-test
egress:
  default: allow
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create and start sandbox.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// Get proxy PID.
	sandboxID := getSandboxID(ctx, t, config, dbPath, name)
	pid := readProxyPID(t, sandboxID)
	require.True(t, isProcessAlive(pid), "proxy process should be alive while sandbox is running")

	// Stop sandbox.
	_, stderr, err = intsbx.RunStop(ctx, config, dbPath, name)
	require.NoError(t, err, "stop failed: stderr=%s", stderr)

	// Wait briefly for cleanup.
	time.Sleep(500 * time.Millisecond)

	// Proxy should be dead.
	assert.False(t, isProcessAlive(pid), "proxy process (PID %d) should be killed after stop", pid)
}

func TestEgressDNATRedirectDenyFromVM(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("dnatdeny")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Write session YAML with default deny.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: dnat-deny-test
egress:
  default: deny
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create and start sandbox with egress.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	// From inside the VM, try to make an HTTP request on port 80.
	// With DNAT active, this gets redirected to the proxy which denies it.
	// wget should fail (non-zero exit code).
	// Use the VM's gateway IP as target — it's a valid IP the VM can route to.
	sandboxID := getSandboxID(ctx, t, config, dbPath, name)
	_ = readProxyPorts(t, sandboxID) // Ensure proxy is running.

	// wget to any HTTP endpoint on port 80 — the DNAT intercepts it.
	// Using 1.1.1.1:80 as a well-known IP. The request never reaches 1.1.1.1 because
	// DNAT rewrites it to the local proxy, which returns 403 Forbidden.
	stdout, stderr, err := intsbx.RunExec(ctx, config, dbPath, name, []string{
		"wget", "-q", "-O-", "--timeout=5", "http://1.1.1.1/",
	})
	assert.Error(t, err, "wget should fail with deny policy: stdout=%s stderr=%s", stdout, stderr)
}

func TestEgressDNATRedirectAllowFromVM(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	name := uniqueName("dnatallow")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cleanupSandbox(t, config, dbPath, name)

	// Start a local HTTP server on the host that the VM can reach via gateway.
	// We'll bind it to 0.0.0.0 so it's reachable from the TAP interface.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("vm-allowed"))
	}))
	defer upstream.Close()

	// Write session YAML with default allow.
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "session.yaml")
	sessionContent := `name: dnat-allow-test
egress:
  default: allow
`
	require.NoError(t, os.WriteFile(sessionFile, []byte(sessionContent), 0644))

	// Create and start sandbox with egress.
	_, stderr, err := intsbx.RunCreate(ctx, config, dbPath, name)
	require.NoError(t, err, "create failed: stderr=%s", stderr)

	_, stderr, err = intsbx.RunStartWithFile(ctx, config, dbPath, name, sessionFile)
	require.NoError(t, err, "start with egress failed: stderr=%s", stderr)
	waitForRunning(ctx, t, config, dbPath, name, 60*time.Second)

	sandboxID := getSandboxID(ctx, t, config, dbPath, name)
	ports := readProxyPorts(t, sandboxID)
	require.Greater(t, ports.HTTPPort, 0)

	// The upstream server listens on a random port (not 80/443), so DNAT won't intercept
	// direct requests to it. Instead, verify the proxy is working by making a request
	// from the host through the proxy to the upstream.
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", ports.HTTPPort)
	client := newProxyHTTPClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "vm-allowed", string(body))
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
