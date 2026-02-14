package sbx_test

import (
	"context"
	"encoding/json"
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
