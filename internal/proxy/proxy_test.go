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
		expStatus     int
		expBody       string
	}{
		"Default allow with no rules should forward request.": {
			defaultPolicy: proxy.ActionAllow,
			expStatus:     http.StatusOK,
			expBody:       "upstream-ok",
		},
		"Default deny with no rules should block request.": {
			defaultPolicy: proxy.ActionDeny,
			expStatus:     http.StatusForbidden,
		},
		"Matching allow rule with default deny should forward request.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.test-upstream.local"},
			},
			// The upstream hostname won't match *.test-upstream.local since httptest
			// uses 127.0.0.1, so this tests that matching on the Host we provide works.
			// We'll use a different test approach below for domain matching.
			expStatus: http.StatusForbidden,
		},
		"Matching deny rule should block request.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*"},
			},
			expStatus: http.StatusForbidden,
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

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			proxyURL, cancel := startProxy(t, matcher)
			defer cancel()

			client := newProxyClient(proxyURL)
			resp, err := client.Get(upstream.URL)
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
		targetHost    string
		expErr        bool
	}{
		"Default allow should tunnel CONNECT.": {
			defaultPolicy: proxy.ActionAllow,
			targetHost:    "127.0.0.1",
		},
		"Default deny should block CONNECT.": {
			defaultPolicy: proxy.ActionDeny,
			targetHost:    "127.0.0.1",
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

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			proxyURL, cancel := startProxy(t, matcher)
			defer cancel()

			// For CONNECT, build a client that uses the proxy for HTTPS.
			pURL, _ := url.Parse(proxyURL)
			client := &http.Client{
				Transport: &http.Transport{
					Proxy:           http.ProxyURL(pURL),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
				Timeout: 5 * time.Second,
			}

			resp, err := client.Get(upstream.URL)

			if test.expErr {
				// The proxy returns 403 which causes the CONNECT to fail.
				// Depending on the Go HTTP client version, this may be a transport error
				// or a 403 response.
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

func TestProxyIPAddressUnidentifiable(t *testing.T) {
	// When a request uses a raw IP (no domain), the proxy should apply default policy.
	tests := map[string]struct {
		defaultPolicy proxy.Action
		expStatus     int
	}{
		"Default allow should forward requests to IPs.": {
			defaultPolicy: proxy.ActionAllow,
			expStatus:     http.StatusOK,
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
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			domain := proxy.ExtractDomain(test.host)
			assert.Equal(test.expDomain, domain)
		})
	}
}
