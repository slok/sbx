package proxy_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/proxy"
)

// startProxy starts a proxy on a random port and returns the proxy URL and a cancel func.
func startProxy(t *testing.T, matcher *proxy.RuleMatcher) (proxyURL string, cancel func()) {
	t.Helper()

	// Get a random free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	p, err := proxy.NewProxy(proxy.ProxyConfig{
		ListenAddr: addr,
		Matcher:    matcher,
		Logger:     log.Noop,
	})
	require.NoError(t, err)

	ctx, ctxCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	// Wait for the proxy to be ready.
	waitForPort(t, addr)

	proxyURL = fmt.Sprintf("http://%s", addr)
	cancel = func() {
		ctxCancel()
		<-done
	}

	return proxyURL, cancel
}

func waitForPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s to be ready", addr)
}

func newProxyClient(proxyURL string) *http.Client {
	pURL, _ := url.Parse(proxyURL)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
}

func TestProxyHTTP(t *testing.T) {
	tests := map[string]struct {
		defaultPolicy proxy.Action
		rules         []proxy.Rule
		requestDomain string
		expStatus     int
		expBody       string
	}{
		"Default allow with no rules should forward request.": {
			defaultPolicy: proxy.ActionAllow,
			requestDomain: "upstream.test",
			expStatus:     http.StatusOK,
			expBody:       "upstream-ok",
		},
		"Default deny with no rules should block request.": {
			defaultPolicy: proxy.ActionDeny,
			requestDomain: "upstream.test",
			expStatus:     http.StatusForbidden,
		},
		"Matching allow rule with default deny should forward request.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "upstream.test"},
			},
			requestDomain: "upstream.test",
			expStatus:     http.StatusOK,
			expBody:       "upstream-ok",
		},
		"Matching deny rule should block request.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*"},
			},
			requestDomain: "upstream.test",
			expStatus:     http.StatusForbidden,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Start a fake upstream.
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("upstream-ok"))
			}))
			defer upstream.Close()

			upstreamURL, _ := url.Parse(upstream.URL)
			upstreamAddr := upstreamURL.Host // "127.0.0.1:PORT"

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			// Start proxy with custom dialer that routes the test domain to our upstream.
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(err)
			proxyAddr := listener.Addr().String()
			listener.Close()

			p, err := proxy.NewProxy(proxy.ProxyConfig{
				ListenAddr: proxyAddr,
				Matcher:    matcher,
				Logger:     log.Noop,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, upstreamAddr)
				},
			})
			require.NoError(err)

			ctx, ctxCancel := context.WithCancel(context.Background())
			defer ctxCancel()

			done := make(chan struct{})
			go func() {
				_ = p.Run(ctx)
				close(done)
			}()
			waitForPort(t, proxyAddr)

			proxyURL := fmt.Sprintf("http://%s", proxyAddr)
			pURL, _ := url.Parse(proxyURL)
			client := &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(pURL),
				},
				Timeout: 5 * time.Second,
			}

			// Use a domain name (not an IP) so the proxy can evaluate domain rules.
			reqURL := fmt.Sprintf("http://%s/", test.requestDomain)
			resp, err := client.Get(reqURL)
			require.NoError(err)
			defer resp.Body.Close()

			assert.Equal(test.expStatus, resp.StatusCode)

			if test.expBody != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(err)
				assert.Equal(test.expBody, string(body))
			}
		})
	}
}

func TestProxyHTTPDomainMatching(t *testing.T) {
	// This test verifies domain matching by sending requests through the proxy
	// with custom Host headers. For denied requests we expect 403. For allowed
	// requests we route the fake domain to a local upstream via a custom resolver.
	tests := map[string]struct {
		defaultPolicy proxy.Action
		rules         []proxy.Rule
		requestHost   string
		expStatus     int
	}{
		"Allowed domain should forward.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "allowed.example.com"},
			},
			requestHost: "allowed.example.com",
			expStatus:   http.StatusOK,
		},
		"Denied domain should block.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "blocked.example.com"},
			},
			requestHost: "blocked.example.com",
			expStatus:   http.StatusForbidden,
		},
		"Wildcard allowed domain should forward subdomains.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.example.com"},
			},
			requestHost: "api.example.com",
			expStatus:   http.StatusOK,
		},
		"Wildcard should not match bare domain.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.example.com"},
			},
			requestHost: "example.com",
			expStatus:   http.StatusForbidden,
		},
		"Trailing dot should be normalized and denied.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "blocked.example.com"},
			},
			requestHost: "blocked.example.com.",
			expStatus:   http.StatusForbidden,
		},
		"Trailing dot should be normalized and allowed.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "allowed.example.com"},
			},
			requestHost: "allowed.example.com.",
			expStatus:   http.StatusOK,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Start upstream.
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}))
			defer upstream.Close()

			// Create a proxy that uses a custom dialer to resolve fake domains
			// to the upstream address. This simulates DNS resolution.
			upstreamURL, _ := url.Parse(upstream.URL)
			upstreamAddr := upstreamURL.Host // "127.0.0.1:PORT"

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			// Get a random free port.
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(err)
			proxyAddr := listener.Addr().String()
			listener.Close()

			ctx, ctxCancel := context.WithCancel(context.Background())
			defer ctxCancel()

			p, err := proxy.NewProxy(proxy.ProxyConfig{
				ListenAddr: proxyAddr,
				Matcher:    matcher,
				Logger:     log.Noop,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, upstreamAddr)
				},
			})
			require.NoError(err)

			done := make(chan struct{})
			go func() {
				_ = p.Run(ctx)
				close(done)
			}()
			waitForPort(t, proxyAddr)

			proxyURL := fmt.Sprintf("http://%s", proxyAddr)
			pURL, _ := url.Parse(proxyURL)
			client := &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(pURL),
				},
				Timeout: 5 * time.Second,
			}

			reqURL := fmt.Sprintf("http://%s/", test.requestHost)
			resp, err := client.Get(reqURL)
			require.NoError(err)
			defer resp.Body.Close()

			assert.Equal(test.expStatus, resp.StatusCode)
		})
	}
}

func TestProxyConnect(t *testing.T) {
	tests := map[string]struct {
		defaultPolicy proxy.Action
		rules         []proxy.Rule
		requestDomain string
		expErr        bool
	}{
		"Default allow should tunnel CONNECT with domain.": {
			defaultPolicy: proxy.ActionAllow,
			requestDomain: "upstream.test",
		},
		"Default deny should block CONNECT with domain.": {
			defaultPolicy: proxy.ActionDeny,
			requestDomain: "upstream.test",
			expErr:        true,
		},
		"Trailing dot on denied domain should block CONNECT.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "upstream.test"},
			},
			requestDomain: "upstream.test.",
			expErr:        true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Start a TLS upstream.
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("tls-ok"))
			}))
			defer upstream.Close()

			upstreamURL, _ := url.Parse(upstream.URL)
			_, upstreamPort, _ := net.SplitHostPort(upstreamURL.Host)

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			// Start proxy with custom dialer that routes domains to our local upstream.
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(err)
			proxyAddr := listener.Addr().String()
			listener.Close()

			p, err := proxy.NewProxy(proxy.ProxyConfig{
				ListenAddr: proxyAddr,
				Matcher:    matcher,
				Logger:     log.Noop,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, "127.0.0.1:"+upstreamPort)
				},
			})
			require.NoError(err)

			ctx, ctxCancel := context.WithCancel(context.Background())
			defer ctxCancel()

			done := make(chan struct{})
			go func() {
				_ = p.Run(ctx)
				close(done)
			}()
			waitForPort(t, proxyAddr)

			proxyURL := fmt.Sprintf("http://%s", proxyAddr)
			pURL, _ := url.Parse(proxyURL)
			client := &http.Client{
				Transport: &http.Transport{
					Proxy:           http.ProxyURL(pURL),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
				Timeout: 5 * time.Second,
			}

			// Use a domain name (not IP) so the proxy can evaluate domain rules.
			// Strip trailing dot for URL (Go's HTTP client handles DNS itself for the CONNECT),
			// but send the raw domain as the CONNECT target through a raw socket test below.
			reqURL := fmt.Sprintf("https://%s:%s/", test.requestDomain, upstreamPort)
			resp, err := client.Get(reqURL)

			if test.expErr {
				// The proxy returns 403 which causes the CONNECT to fail.
				if err != nil {
					assert.Error(err)
				} else {
					defer resp.Body.Close()
					assert.Equal(http.StatusForbidden, resp.StatusCode)
				}
			} else {
				require.NoError(err)
				defer resp.Body.Close()
				assert.Equal(http.StatusOK, resp.StatusCode)

				body, err := io.ReadAll(resp.Body)
				require.NoError(err)
				assert.Equal("tls-ok", string(body))
			}
		})
	}
}

func TestProxyCONNECTTrailingDotBlocked(t *testing.T) {
	// A CONNECT request with a trailing dot (FQDN form) must be correctly
	// matched against deny rules. This uses raw sockets to ensure the trailing
	// dot is sent verbatim, since Go's http.Client may normalize it away.
	assert := assert.New(t)
	require := require.New(t)

	matcher, err := proxy.NewRuleMatcher(proxy.ActionAllow, []proxy.Rule{
		{Action: proxy.ActionDeny, Domain: "blocked.test"},
	})
	require.NoError(err)

	proxyURL, cancel := startProxy(t, matcher)
	defer cancel()

	// Parse proxy address from URL.
	pURL, _ := url.Parse(proxyURL)
	proxyAddr := pURL.Host

	// Send raw CONNECT with trailing dot.
	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	require.NoError(err)
	defer conn.Close()

	_, err = fmt.Fprintf(conn, "CONNECT blocked.test.:443 HTTP/1.1\r\nHost: blocked.test.:443\r\n\r\n")
	require.NoError(err)

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	require.NoError(err)
	resp := string(buf[:n])

	assert.Contains(resp, "403 Forbidden", "CONNECT with trailing dot should be denied")
}

func TestProxyIPAddressUnidentifiable(t *testing.T) {
	// When a request uses a raw IP (no domain), the proxy should always block it.
	// IP-based requests bypass domain filtering entirely, so they must be denied
	// regardless of default policy.
	tests := map[string]struct {
		defaultPolicy proxy.Action
		expStatus     int
	}{
		"Default allow should still block requests to IPs.": {
			defaultPolicy: proxy.ActionAllow,
			expStatus:     http.StatusForbidden,
		},
		"Default deny should block requests to IPs.": {
			defaultPolicy: proxy.ActionDeny,
			expStatus:     http.StatusForbidden,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}))
			defer upstream.Close()

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, nil)
			require.NoError(err)

			proxyURL, cancel := startProxy(t, matcher)
			defer cancel()

			// upstream.URL is http://127.0.0.1:PORT -- raw IP, no domain.
			client := newProxyClient(proxyURL)
			resp, err := client.Get(upstream.URL)
			require.NoError(err)
			defer resp.Body.Close()

			assert.Equal(test.expStatus, resp.StatusCode)
		})
	}
}

func TestProxyCONNECTIPAddressBlocked(t *testing.T) {
	// CONNECT to an IP address (no domain) should always be blocked, regardless
	// of default policy. This prevents attackers from bypassing domain-based TLS/SNI
	// filtering by establishing a CONNECT tunnel directly to an IP address.
	tests := map[string]struct {
		defaultPolicy proxy.Action
	}{
		"Default allow should still block CONNECT to IP.": {
			defaultPolicy: proxy.ActionAllow,
		},
		"Default deny should block CONNECT to IP.": {
			defaultPolicy: proxy.ActionDeny,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Start a TLS upstream on a raw IP (127.0.0.1).
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("should-not-reach"))
			}))
			defer upstream.Close()

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, nil)
			require.NoError(err)

			proxyURL, cancel := startProxy(t, matcher)
			defer cancel()

			// upstream.URL is https://127.0.0.1:PORT — CONNECT target is an IP.
			pURL, _ := url.Parse(proxyURL)
			client := &http.Client{
				Transport: &http.Transport{
					Proxy:           http.ProxyURL(pURL),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
				Timeout: 5 * time.Second,
			}

			resp, err := client.Get(upstream.URL)

			// The proxy returns 403 which causes CONNECT to fail.
			if err != nil {
				assert.Error(err)
			} else {
				defer resp.Body.Close()
				assert.Equal(http.StatusForbidden, resp.StatusCode)
			}
		})
	}
}

func TestProxyCONNECTDomainAllowed(t *testing.T) {
	// CONNECT to a domain name should still work when the domain is allowed.
	// This verifies the IP-blocking fix doesn't break legitimate CONNECT tunnels.
	assert := assert.New(t)
	require := require.New(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tls-ok"))
	}))
	defer upstream.Close()

	upstreamURL, _ := url.Parse(upstream.URL)
	_, upstreamPort, _ := net.SplitHostPort(upstreamURL.Host)

	matcher, err := proxy.NewRuleMatcher(proxy.ActionAllow, nil)
	require.NoError(err)

	// Get a random free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	proxyAddr := listener.Addr().String()
	listener.Close()

	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	p, err := proxy.NewProxy(proxy.ProxyConfig{
		ListenAddr: proxyAddr,
		Matcher:    matcher,
		Logger:     log.Noop,
		// Route the allowed domain to our local TLS upstream.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, "127.0.0.1:"+upstreamPort)
		},
	})
	require.NoError(err)

	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()
	waitForPort(t, proxyAddr)

	proxyURL := fmt.Sprintf("http://%s", proxyAddr)
	pURL, _ := url.Parse(proxyURL)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	// Use a domain name (not IP) — this should work.
	resp, err := client.Get(fmt.Sprintf("https://allowed.example.com:%s/", upstreamPort))
	require.NoError(err)
	defer resp.Body.Close()

	assert.Equal(http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(err)
	assert.Equal("tls-ok", string(body))
}

func TestExtractDomain(t *testing.T) {
	tests := map[string]struct {
		host      string
		expDomain string
	}{
		"Domain with port.": {
			host:      "github.com:443",
			expDomain: "github.com",
		},
		"Domain without port.": {
			host:      "github.com",
			expDomain: "github.com",
		},
		"IP with port returns empty.": {
			host:      "1.2.3.4:443",
			expDomain: "",
		},
		"IP without port returns empty.": {
			host:      "1.2.3.4",
			expDomain: "",
		},
		"IPv6 with port returns empty.": {
			host:      "[::1]:443",
			expDomain: "",
		},
		"Empty host returns empty.": {
			host:      "",
			expDomain: "",
		},
		"Domain with spaces is trimmed.": {
			host:      " github.com:443 ",
			expDomain: "github.com",
		},
		"Uppercase domain is lowered.": {
			host:      "GitHub.COM:443",
			expDomain: "github.com",
		},
		"Trailing dot is stripped (FQDN normalization).": {
			host:      "github.com.",
			expDomain: "github.com",
		},
		"Trailing dot with port is stripped.": {
			host:      "github.com.:443",
			expDomain: "github.com",
		},
		"Trailing dot with uppercase is normalized.": {
			host:      "GitHub.COM.",
			expDomain: "github.com",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			domain := proxy.ExtractDomain(test.host)
			assert.Equal(test.expDomain, domain)
		})
	}
}
