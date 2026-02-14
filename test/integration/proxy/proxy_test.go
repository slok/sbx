package proxy_test

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intproxy "github.com/slok/sbx/test/integration/proxy"
)

func TestProxyDefaultAllow(t *testing.T) {
	config := intproxy.NewConfig(t)

	// Start a local HTTP server as the upstream target.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("upstream-response"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "allow", nil)
	defer cancel()

	// HTTP request through the proxy should succeed (default allow, no rules).
	// Use domain-based URL (localhost) instead of raw IP — the proxy blocks IP-based requests.
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(ipToLocalhostURL(upstream.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "upstream-response", string(body))
}

func TestProxyDefaultDeny(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should-not-reach"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", nil)
	defer cancel()

	// HTTP request through the proxy should be blocked (default deny, no rules).
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestProxyAllowRuleWithDenyDefault(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("allowed"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	// Default deny, but allow localhost via rule.
	// This tests that an allow rule overrides default deny.
	rules := []string{
		`{"action":"allow","domain":"localhost"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", rules)
	defer cancel()

	// Use domain-based URL (localhost) instead of raw IP.
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(ipToLocalhostURL(upstream.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "allowed", string(body))
}

func TestProxyDenyRule(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	rules := []string{
		`{"action":"deny","domain":"*"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "allow", rules)
	defer cancel()

	// Even with default allow, the catch-all deny rule should block.
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestProxyCONNECTAllow(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tls-response"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "allow", nil)
	defer cancel()

	// HTTPS request through CONNECT should tunnel successfully.
	// Use domain-based URL (localhost) instead of raw IP — the proxy blocks IP-based CONNECT.
	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(ipToLocalhostURL(upstream.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "tls-response", string(body))
}

func TestProxyCONNECTDeny(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", nil)
	defer cancel()

	// HTTPS CONNECT should fail (default deny).
	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		// CONNECT failure may surface as a transport error.
		assert.Error(t, err)
	} else {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}
}

func TestProxyMultipleRulesFirstMatchWins(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	// First rule: deny *.blocked.test.
	// Second rule: allow * (catches everything).
	// Third rule: deny * (should not be reached because 2nd rule matches first).
	// Default deny. First match wins: "allow *" should let the request through.
	rules := []string{
		`{"action":"deny","domain":"*.blocked.test"}`,
		`{"action":"allow","domain":"*"}`,
		`{"action":"deny","domain":"*"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", rules)
	defer cancel()

	// Use domain-based URL (localhost) instead of raw IP — the proxy blocks IP-based requests.
	// localhost matches "allow *" (2nd rule) before "deny *" (3rd rule).
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(ipToLocalhostURL(upstream.URL))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "reached", string(body))
}

func TestProxyWildcardRule(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("wildcard-ok"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	// Allow *.example.com pattern — won't match raw IP addresses.
	// The proxy blocks IP-based requests outright (403), so the upstream URL
	// (http://127.0.0.1:PORT) never even reaches rule matching.
	rules := []string{
		`{"action":"allow","domain":"*.example.com"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", rules)
	defer cancel()

	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// upstream.URL is http://127.0.0.1:PORT — an IP address, blocked by the proxy with 403.
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func newProxyClient(proxyAddr string) *http.Client {
	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(pURL),
		},
		Timeout: 5 * time.Second,
	}
}

// ipToLocalhostURL replaces the IP (e.g. "127.0.0.1") in an httptest server URL with "localhost".
// This is needed because the proxy blocks requests to raw IP addresses; using "localhost"
// (which resolves to 127.0.0.1) lets the request reach the local upstream server via a domain name.
func ipToLocalhostURL(rawURL string) string {
	return strings.Replace(rawURL, "127.0.0.1", "localhost", 1)
}

// startFakeDNSUpstream starts a local DNS server (UDP + TCP) that answers A queries with 93.184.216.34.
// Returns the address and a cleanup function.
func startFakeDNSUpstream(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	// Find a port that is free on both UDP and TCP.
	upstreamPort := intproxy.GetFreeDualPort(t)
	upstreamAddr := fmt.Sprintf("127.0.0.1:%d", upstreamPort)

	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(r)
		if len(r.Question) > 0 && r.Question[0].Qtype == dns.TypeA {
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: net.ParseIP("93.184.216.34"),
			})
		}
		_ = w.WriteMsg(resp)
	})

	udpServer := &dns.Server{
		Addr:              upstreamAddr,
		Net:               "udp",
		Handler:           mux,
		NotifyStartedFunc: func() {},
	}
	tcpServer := &dns.Server{
		Addr:              upstreamAddr,
		Net:               "tcp",
		Handler:           mux,
		NotifyStartedFunc: func() {},
	}

	go func() { _ = udpServer.ListenAndServe() }()
	go func() { _ = tcpServer.ListenAndServe() }()

	// Wait for upstream to be ready (checks UDP).
	intproxy.WaitForDNSPort(t, upstreamAddr, 3*time.Second)

	return upstreamAddr, func() {
		_ = udpServer.Shutdown()
		_ = tcpServer.Shutdown()
	}
}

func dnsQuery(t *testing.T, addr, domain string, qtype uint16) *dns.Msg {
	t.Helper()
	return dnsQueryProto(t, addr, domain, qtype, "")
}

func dnsQueryTCP(t *testing.T, addr, domain string, qtype uint16) *dns.Msg {
	t.Helper()
	return dnsQueryProto(t, addr, domain, qtype, "tcp")
}

func dnsQueryProto(t *testing.T, addr, domain string, qtype uint16, net string) *dns.Msg {
	t.Helper()

	c := &dns.Client{
		Timeout: 2 * time.Second,
		Net:     net, // "" = UDP (default), "tcp" = TCP.
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)

	resp, _, err := c.Exchange(m, addr)
	require.NoError(t, err)
	return resp
}

func TestProxyTrailingDotHTTPDeny(t *testing.T) {
	// A trailing dot in the Host header (FQDN form) must NOT bypass deny rules.
	// This is a regression test for an egress bypass where "blocked.test." would
	// pass through a deny rule for "blocked.test".
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should-not-reach"))
	}))
	defer upstream.Close()

	port := intproxy.GetFreePort(t)
	rules := []string{
		`{"action":"deny","domain":"localhost"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "allow", rules)
	defer cancel()

	// Send a raw HTTP request with trailing dot via the proxy.
	// We use raw sockets because Go's http.Client may strip the trailing dot.
	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	// Proxy-style HTTP request with absolute URI and trailing dot in Host.
	upstreamURL := strings.Replace(upstream.URL, "127.0.0.1", "localhost.", 1)
	req := fmt.Sprintf("GET %s/ HTTP/1.1\r\nHost: localhost.\r\nConnection: close\r\n\r\n", upstreamURL)
	_, err = conn.Write([]byte(req))
	require.NoError(t, err)

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	resp := string(buf[:n])

	assert.Contains(t, resp, "403 Forbidden",
		"HTTP request with trailing dot should be denied by the proxy")
}

func TestProxyTrailingDotCONNECTDeny(t *testing.T) {
	// A trailing dot in the CONNECT target must NOT bypass deny rules.
	config := intproxy.NewConfig(t)

	port := intproxy.GetFreePort(t)
	rules := []string{
		`{"action":"deny","domain":"blocked.test"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "allow", rules)
	defer cancel()

	// Send raw CONNECT with trailing dot.
	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()

	_, err = fmt.Fprintf(conn, "CONNECT blocked.test.:443 HTTP/1.1\r\nHost: blocked.test.:443\r\n\r\n")
	require.NoError(t, err)

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	resp := string(buf[:n])

	assert.Contains(t, resp, "403 Forbidden",
		"CONNECT with trailing dot should be denied by the proxy")
}

func TestDNSProxyDefaultAllow(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "allow", nil)
	defer cancel()

	// DNS query should be forwarded (default allow).
	resp := dnsQuery(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)
}

func TestDNSProxyDefaultDeny(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "deny", nil)
	defer cancel()

	// DNS query should be refused (default deny).
	resp := dnsQuery(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestDNSProxyAllowRule(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	rules := []string{
		`{"action":"allow","domain":"allowed.example.com"}`,
	}
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "deny", rules)
	defer cancel()

	// Allowed domain should be forwarded.
	resp := dnsQuery(t, dnsAddr, "allowed.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)

	// Non-matching domain should be refused.
	resp = dnsQuery(t, dnsAddr, "blocked.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestDNSProxyWildcardRule(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	rules := []string{
		`{"action":"allow","domain":"*.example.com"}`,
	}
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "deny", rules)
	defer cancel()

	// Subdomain should match wildcard and be forwarded.
	resp := dnsQuery(t, dnsAddr, "api.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)

	// Bare domain should NOT match *.example.com and be refused.
	resp = dnsQuery(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestDNSProxyTCPDefaultAllow(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "allow", nil)
	defer cancel()

	// DNS-over-TCP query should be forwarded (default allow).
	resp := dnsQueryTCP(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)
}

func TestDNSProxyTCPDefaultDeny(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "deny", nil)
	defer cancel()

	// DNS-over-TCP query should be refused (default deny).
	resp := dnsQueryTCP(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestDNSProxyTCPAllowRule(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	httpPort := intproxy.GetFreePort(t)
	dnsPort := intproxy.GetFreeDualPort(t)
	rules := []string{
		`{"action":"allow","domain":"allowed.example.com"}`,
	}
	_, dnsAddr, cancel := intproxy.StartProxyWithDNS(t, config, httpPort, dnsPort, upstreamAddr, "deny", rules)
	defer cancel()

	// Allowed domain should be forwarded over TCP.
	resp := dnsQueryTCP(t, dnsAddr, "allowed.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)

	// Non-matching domain should be refused over TCP.
	resp = dnsQueryTCP(t, dnsAddr, "blocked.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}
