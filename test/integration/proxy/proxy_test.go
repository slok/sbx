package proxy_test

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
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
	// Default deny, but allow everything via wildcard rule.
	// This tests that an allow rule overrides default deny.
	rules := []string{
		`{"action":"allow","domain":"*"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", rules)
	defer cancel()

	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
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
	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(upstream.URL)
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
	// First rule: allow *.example.com (won't match IP-based upstream).
	// Second rule: allow * (catches everything).
	// Third rule: deny * (should not be reached).
	// Default deny. First match wins: allow * should let the request through.
	rules := []string{
		`{"action":"deny","domain":"*.blocked.test"}`,
		`{"action":"allow","domain":"*"}`,
		`{"action":"deny","domain":"*"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", rules)
	defer cancel()

	// Request to IP-based upstream: matches "allow *" (2nd rule) before "deny *" (3rd).
	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
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
	// Allow *.0.0.1 pattern -- won't match 127.0.0.1 (it's an IP, not a domain).
	// With default deny and IP-based upstream, request should be blocked.
	rules := []string{
		`{"action":"allow","domain":"*.example.com"}`,
	}
	proxyAddr, cancel := intproxy.StartProxy(t, config, port, "deny", rules)
	defer cancel()

	client := newProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// upstream.URL is http://127.0.0.1:PORT -- an IP, not a domain.
	// IP = unidentifiable domain, falls to default deny.
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
