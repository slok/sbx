package proxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/slok/sbx/test/integration/testutils"
)

// Config holds proxy integration test configuration.
type Config struct {
	Binary string
}

func (c *Config) defaults() error {
	if c.Binary == "" {
		c.Binary = "sbx"
	}

	if !filepath.IsAbs(c.Binary) {
		return fmt.Errorf("SBX_INTEGRATION_BINARY must be an absolute path, got %q", c.Binary)
	}
	if _, err := os.Stat(c.Binary); err != nil {
		return fmt.Errorf("sbx binary not found at %q: %w", c.Binary, err)
	}

	return nil
}

// NewConfig loads proxy integration test configuration from environment variables.
func NewConfig(t *testing.T) Config {
	t.Helper()

	const (
		envActivation = "SBX_INTEGRATION_PROXY"
		envBinary     = "SBX_INTEGRATION_BINARY"
	)

	if os.Getenv(envActivation) != "true" {
		t.Skipf("Skipping proxy integration test: %s is not set to 'true'", envActivation)
	}

	c := Config{
		Binary: os.Getenv(envBinary),
	}

	if err := c.defaults(); err != nil {
		t.Skipf("Skipping due to invalid config: %s", err)
	}

	return c
}

// GetFreePort returns an available TCP port on localhost.
func GetFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// WaitForPort waits until a TCP port is accepting connections.
func WaitForPort(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s to be ready", addr)
}

// StartProxy starts the sbx internal-vm-proxy command in the background.
// Returns the proxy address and a cancel function to stop it.
func StartProxy(t *testing.T, config Config, port int, defaultPolicy string, rules []string) (proxyAddr string, cancel func()) {
	t.Helper()

	ctx, ctxCancel := context.WithCancel(context.Background())

	args := []string{
		"--no-log",
		"internal-vm-proxy",
		"--port", fmt.Sprintf("%d", port),
		"--default-policy", defaultPolicy,
	}
	for _, r := range rules {
		args = append(args, "--rule", r)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = testutils.RunSBXArgs(ctx, nil, config.Binary, args, true)
	}()

	proxyAddr = fmt.Sprintf("127.0.0.1:%d", port)
	WaitForPort(t, proxyAddr, 5*time.Second)

	cancel = func() {
		ctxCancel()
		<-done
	}

	return proxyAddr, cancel
}

// GetFreeUDPPort returns an available UDP port on localhost.
func GetFreeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not get free UDP port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	return port
}

// WaitForDNSPort waits until a DNS server is responding on the given address.
func WaitForDNSPort(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	c := new(dns.Client)
	c.Timeout = 200 * time.Millisecond

	for time.Now().Before(deadline) {
		m := new(dns.Msg)
		m.SetQuestion("test.", dns.TypeA)
		_, _, err := c.Exchange(m, addr)
		if err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for DNS server at %s to be ready", addr)
}

// StartProxyWithDNS starts the sbx internal-vm-proxy command with DNS proxy enabled.
// Returns the HTTP proxy address, DNS proxy address, and a cancel function to stop both.
func StartProxyWithDNS(t *testing.T, config Config, httpPort, dnsPort int, dnsUpstream, defaultPolicy string, rules []string) (proxyAddr, dnsAddr string, cancel func()) {
	t.Helper()

	ctx, ctxCancel := context.WithCancel(context.Background())

	args := []string{
		"--no-log",
		"internal-vm-proxy",
		"--port", fmt.Sprintf("%d", httpPort),
		"--dns-port", fmt.Sprintf("%d", dnsPort),
		"--dns-upstream", dnsUpstream,
		"--default-policy", defaultPolicy,
	}
	for _, r := range rules {
		args = append(args, "--rule", r)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = testutils.RunSBXArgs(ctx, nil, config.Binary, args, true)
	}()

	proxyAddr = fmt.Sprintf("127.0.0.1:%d", httpPort)
	dnsAddr = fmt.Sprintf("127.0.0.1:%d", dnsPort)
	WaitForPort(t, proxyAddr, 5*time.Second)
	WaitForDNSPort(t, dnsAddr, 5*time.Second)

	cancel = func() {
		ctxCancel()
		<-done
	}

	return proxyAddr, dnsAddr, cancel
}
