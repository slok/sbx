package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
)

func TestExtractSNIFromClientHello(t *testing.T) {
	tests := map[string]struct {
		sni    string
		expSNI string
	}{
		"Standard domain.": {
			sni:    "example.com",
			expSNI: "example.com",
		},
		"Subdomain.": {
			sni:    "api.github.com",
			expSNI: "api.github.com",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// Generate a TLS ClientHello by starting a TLS handshake to a server.
			clientHello := captureClientHello(t, test.sni)

			got := extractSNIFromClientHello(clientHello)
			assert.Equal(test.expSNI, got)
		})
	}
}

func TestTLSProxy_DenyCloses(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	matcher, err := NewRuleMatcher(ActionDeny, nil)
	require.NoError(err)

	// Start TLS proxy.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	proxyAddr := listener.Addr().String()
	listener.Close()

	tlsProxy, err := NewTLSProxy(TLSProxyConfig{
		ListenAddr: proxyAddr,
		Matcher:    matcher,
	})
	require.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = tlsProxy.Run(ctx) }()

	// Wait for proxy to be ready.
	waitForTCPPort(t, proxyAddr, 3*time.Second)

	// Try to connect with TLS — should be rejected (connection closed).
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 2 * time.Second},
		"tcp",
		proxyAddr,
		&tls.Config{
			ServerName:         "denied.example.com",
			InsecureSkipVerify: true,
		},
	)
	if err == nil {
		conn.Close()
		assert.Fail("expected TLS connection to be rejected by deny policy")
	}
	// Error is expected — the proxy closes the connection.
}

func TestTLSProxy_AllowTunnels(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	matcher, err := NewRuleMatcher(ActionAllow, nil)
	require.NoError(err)

	// Start a real TLS server as the target.
	targetCert := generateSelfSignedCert(t, "allowed.example.com")
	targetListener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{targetCert},
	})
	require.NoError(err)
	defer targetListener.Close()
	targetAddr := targetListener.Addr().String()

	// Accept one connection and echo back.
	go func() {
		conn, err := targetListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if n > 0 {
			_, _ = conn.Write(buf[:n])
		}
	}()

	// Start TLS proxy that routes to the target.
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	proxyAddr := proxyListener.Addr().String()
	proxyListener.Close()

	_, targetPort, _ := net.SplitHostPort(targetAddr)

	tlsProxy, err := NewTLSProxy(TLSProxyConfig{
		ListenAddr: proxyAddr,
		Matcher:    matcher,
		// Override dial to connect to our local target instead of the SNI domain.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, "127.0.0.1:"+targetPort)
		},
	})
	require.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = tlsProxy.Run(ctx) }()
	waitForTCPPort(t, proxyAddr, 3*time.Second)

	// Connect through the proxy with TLS — should tunnel to the target.
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 2 * time.Second},
		"tcp",
		proxyAddr,
		&tls.Config{
			ServerName:         "allowed.example.com",
			InsecureSkipVerify: true,
		},
	)
	require.NoError(err)
	defer conn.Close()

	// Send data and expect echo.
	msg := []byte("hello through proxy")
	_, err = conn.Write(msg)
	require.NoError(err)

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	require.NoError(err)
	assert.Equal("hello through proxy", string(buf[:n]))
}

// captureClientHello generates a TLS ClientHello for the given SNI by initiating
// a handshake against a local listener that captures the raw bytes.
func captureClientHello(t *testing.T, sni string) []byte {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	captured := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			captured <- nil
			return
		}
		defer conn.Close()

		// Read the TLS record header (5 bytes) + body.
		header := make([]byte, 5)
		if _, err := conn.Read(header); err != nil {
			captured <- nil
			return
		}
		recordLen := int(header[3])<<8 | int(header[4])
		body := make([]byte, recordLen)
		if _, err := conn.Read(body); err != nil {
			captured <- nil
			return
		}
		captured <- append(header, body...)
	}()

	// Initiate a TLS handshake (will fail, but the ClientHello is captured).
	conn, _ := tls.DialWithDialer(
		&net.Dialer{Timeout: 1 * time.Second},
		"tcp",
		listener.Addr().String(),
		&tls.Config{
			ServerName:         sni,
			InsecureSkipVerify: true,
		},
	)
	if conn != nil {
		conn.Close()
	}

	result := <-captured
	require.NotNil(t, result, "failed to capture ClientHello")
	return result
}

// waitForTCPPort waits until a TCP port is accepting connections.
func waitForTCPPort(t *testing.T, addr string, timeout time.Duration) {
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

// generateSelfSignedCert generates a self-signed TLS certificate for testing.
func generateSelfSignedCert(t *testing.T, cn string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		DNSNames:     []string{cn},
	}

	pub := key.Public()
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, pub, key)
	require.NoError(t, err)

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}

// TestPeekClientHelloSNI_NotTLS verifies error on non-TLS data.
func TestPeekClientHelloSNI_NotTLS(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	go func() {
		_, _ = client.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		client.Close()
	}()

	_, _, err := peekClientHelloSNI(server)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a TLS handshake")
}

func TestTLSProxy_DomainRuleCheck(t *testing.T) {
	tests := map[string]struct {
		defaultPolicy Action
		rules         []Rule
		sni           string
		lookupHost    LookupHostFunc
		expectConnect bool
	}{
		"Allow all, connect succeeds.": {
			defaultPolicy: ActionAllow,
			sni:           "allowed.example.com",
			expectConnect: true,
		},
		"Deny all, connect fails.": {
			defaultPolicy: ActionDeny,
			sni:           "denied.example.com",
			expectConnect: false,
		},
		"Deny default but allow rule matches.": {
			defaultPolicy: ActionDeny,
			rules:         []Rule{{Action: ActionAllow, Domain: "*.example.com"}},
			sni:           "api.example.com",
			expectConnect: true,
		},
		"Allow default but deny rule matches.": {
			defaultPolicy: ActionAllow,
			rules:         []Rule{{Action: ActionDeny, Domain: "evil.com"}},
			sni:           "evil.com",
			expectConnect: false,
		},
		"Trailing dot on denied domain should be blocked.": {
			defaultPolicy: ActionAllow,
			rules:         []Rule{{Action: ActionDeny, Domain: "evil.com"}},
			sni:           "evil.com.",
			expectConnect: false,
		},
		"Trailing dot on allowed domain should connect.": {
			defaultPolicy: ActionDeny,
			rules:         []Rule{{Action: ActionAllow, Domain: "good.example.com"}},
			sni:           "good.example.com.",
			expectConnect: true,
		},
		"SNI domain resolving to denied domain IP should be blocked.": {
			defaultPolicy: ActionAllow,
			rules:         []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:           "140-82-121-4.sslip.io",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				// Simulate sslip.io resolving to GitHub's IP.
				switch host {
				case "140-82-121-4.sslip.io":
					return []string{"140.82.121.4"}, nil
				case "github.com":
					return []string{"140.82.121.4"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expectConnect: false,
		},
		"SNI domain resolving to different IP than denied domain should connect.": {
			defaultPolicy: ActionAllow,
			rules:         []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:           "safe.example.com",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				switch host {
				case "safe.example.com":
					return []string{"93.184.216.34"}, nil
				case "github.com":
					return []string{"140.82.121.4"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expectConnect: true,
		},
		"SNI DNS failure should still allow (fail-open).": {
			defaultPolicy: ActionAllow,
			rules:         []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:           "unresolvable.example.com",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				return nil, fmt.Errorf("no such host")
			},
			expectConnect: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			matcher, err := NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			// Use a clean SNI for the cert (strip trailing dot).
			certSNI := test.sni
			if certSNI[len(certSNI)-1] == '.' {
				certSNI = certSNI[:len(certSNI)-1]
			}

			// Start a TLS target server.
			targetCert := generateSelfSignedCert(t, certSNI)
			targetListener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
				Certificates: []tls.Certificate{targetCert},
			})
			require.NoError(err)
			defer targetListener.Close()

			go func() {
				conn, err := targetListener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				// Hold connection open to allow TLS handshake to complete.
				buf := make([]byte, 1)
				_, _ = conn.Read(buf)
			}()

			_, targetPort, _ := net.SplitHostPort(targetListener.Addr().String())

			proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(err)
			proxyAddr := proxyListener.Addr().String()
			proxyListener.Close()

			cfg := TLSProxyConfig{
				ListenAddr: proxyAddr,
				Matcher:    matcher,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%s", targetPort))
				},
			}
			if test.lookupHost != nil {
				cfg.LookupHost = test.lookupHost
			}

			tlsProxy, err := NewTLSProxy(cfg)
			require.NoError(err)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() { _ = tlsProxy.Run(ctx) }()
			waitForTCPPort(t, proxyAddr, 3*time.Second)

			conn, err := tls.DialWithDialer(
				&net.Dialer{Timeout: 2 * time.Second},
				"tcp",
				proxyAddr,
				&tls.Config{
					ServerName:         test.sni,
					InsecureSkipVerify: true,
				},
			)

			if test.expectConnect {
				require.NoError(err, "expected TLS connection to succeed")
				conn.Close()
			} else {
				if err == nil {
					conn.Close()
				}
				assert.Error(t, err, "expected TLS connection to be denied")
			}
		})
	}
}

func TestTLSProxy_IsDeniedByIPOverlap(t *testing.T) {
	tests := map[string]struct {
		rules      []Rule
		sni        string
		lookupHost LookupHostFunc
		expDenied  bool
	}{
		"No denied domains should not block.": {
			rules: nil,
			sni:   "safe.example.com",
			lookupHost: func(_ context.Context, _ string) ([]string, error) {
				return []string{"1.2.3.4"}, nil
			},
			expDenied: false,
		},
		"SNI resolving to same IP as denied domain should block.": {
			rules: []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:   "140-82-121-4.sslip.io",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				switch host {
				case "140-82-121-4.sslip.io":
					return []string{"140.82.121.4"}, nil
				case "github.com":
					return []string{"140.82.121.4"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expDenied: true,
		},
		"SNI resolving to different IP should not block.": {
			rules: []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:   "safe.example.com",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				switch host {
				case "safe.example.com":
					return []string{"93.184.216.34"}, nil
				case "github.com":
					return []string{"140.82.121.4"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expDenied: false,
		},
		"SNI DNS failure should not block (fail-open).": {
			rules: []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:   "unresolvable.example.com",
			lookupHost: func(_ context.Context, _ string) ([]string, error) {
				return nil, fmt.Errorf("no such host")
			},
			expDenied: false,
		},
		"Denied domain DNS failure should not block that domain.": {
			rules: []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:   "safe.example.com",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				if host == "safe.example.com" {
					return []string{"1.2.3.4"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expDenied: false,
		},
		"Multiple denied domains with partial IP overlap should block.": {
			rules: []Rule{
				{Action: ActionDeny, Domain: "github.com"},
				{Action: ActionDeny, Domain: "evil.com"},
			},
			sni: "sneaky.sslip.io",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				switch host {
				case "sneaky.sslip.io":
					return []string{"6.6.6.6"}, nil
				case "github.com":
					return []string{"140.82.121.4"}, nil
				case "evil.com":
					return []string{"6.6.6.6"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expDenied: true,
		},
		"Wildcard deny rules should be ignored for IP overlap.": {
			rules: []Rule{{Action: ActionDeny, Domain: "*.github.com"}},
			sni:   "140-82-121-4.sslip.io",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				return []string{"140.82.121.4"}, nil
			},
			expDenied: false,
		},
		"SNI with multiple IPs where one overlaps should block.": {
			rules: []Rule{{Action: ActionDeny, Domain: "github.com"}},
			sni:   "multi-ip.example.com",
			lookupHost: func(_ context.Context, host string) ([]string, error) {
				switch host {
				case "multi-ip.example.com":
					return []string{"1.2.3.4", "140.82.121.4"}, nil
				case "github.com":
					return []string{"140.82.121.4"}, nil
				}
				return nil, fmt.Errorf("no such host")
			},
			expDenied: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			matcher, err := NewRuleMatcher(ActionAllow, test.rules)
			require.NoError(err)

			proxy := &TLSProxy{
				matcher:    matcher,
				lookupHost: test.lookupHost,
				logger:     log.Noop,
			}

			got := proxy.isDeniedByIPOverlap(context.Background(), test.sni, "127.0.0.1:12345")
			assert.Equal(test.expDenied, got)
		})
	}
}

func TestExtractCertDomains(t *testing.T) {
	tests := map[string]struct {
		cn         string
		sans       []string
		expDomains []string
	}{
		"Certificate with CN and SANs.": {
			cn:         "github.com",
			sans:       []string{"github.com", "www.github.com"},
			expDomains: []string{"github.com", "www.github.com", "github.com"},
		},
		"Certificate with only SANs.": {
			cn:         "",
			sans:       []string{"api.example.com"},
			expDomains: []string{"api.example.com"},
		},
		"Certificate with wildcard SAN.": {
			cn:         "example.com",
			sans:       []string{"*.example.com", "example.com"},
			expDomains: []string{"*.example.com", "example.com", "example.com"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// Generate a test certificate.
			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			template := x509.Certificate{
				SerialNumber: big.NewInt(1),
				Subject:      pkix.Name{CommonName: test.cn},
				NotBefore:    time.Now(),
				NotAfter:     time.Now().Add(1 * time.Hour),
				DNSNames:     test.sans,
			}
			certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)

			// Build a TLS Certificate handshake message body:
			// certificates_length (3) + certificate_length (3) + certificate_data
			certLen := len(certDER)
			totalLen := 3 + certLen
			certMsg := make([]byte, 3+totalLen)
			certMsg[0] = byte(totalLen >> 16)
			certMsg[1] = byte(totalLen >> 8)
			certMsg[2] = byte(totalLen)
			certMsg[3] = byte(certLen >> 16)
			certMsg[4] = byte(certLen >> 8)
			certMsg[5] = byte(certLen)
			copy(certMsg[6:], certDER)

			domains := extractCertDomains(certMsg)
			assert.Equal(test.expDomains, domains)
		})
	}
}

func TestExtractCertDomains_InvalidInput(t *testing.T) {
	// Empty or garbage input should return nil.
	assert.Nil(t, extractCertDomains(nil))
	assert.Nil(t, extractCertDomains([]byte{}))
	assert.Nil(t, extractCertDomains([]byte{0, 0}))
	assert.Nil(t, extractCertDomains([]byte{0, 0, 5, 0, 0})) // claims 5 bytes but only 2 available.
}

func TestPeekAndCheckServerCert_BlocksDeniedCertDomain(t *testing.T) {
	// Simulate a TLS 1.2 server that sends ServerHello + Certificate with github.com SAN.
	// The proxy should detect github.com in the cert and block the connection.
	require := require.New(t)

	matcher, err := NewRuleMatcher(ActionAllow, []Rule{{Action: ActionDeny, Domain: "github.com"}})
	require.NoError(err)

	// Create a TLS server with a github.com cert.
	cert := generateSelfSignedCert(t, "github.com")
	serverListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	defer serverListener.Close()

	go func() {
		rawConn, err := serverListener.Accept()
		if err != nil {
			return
		}
		defer rawConn.Close()
		// Wrap in TLS and do the handshake (which sends ServerHello + Certificate).
		tlsConn := tls.Server(rawConn, &tls.Config{
			Certificates: []tls.Certificate{cert},
			// Force TLS 1.2 so the certificate is visible in plaintext.
			MaxVersion: tls.VersionTLS12,
		})
		_ = tlsConn.Handshake()
		// Hold open.
		buf := make([]byte, 1)
		_, _ = tlsConn.Read(buf)
	}()

	// Connect and send a ClientHello.
	serverAddr := serverListener.Addr().String()
	conn, err := net.DialTimeout("tcp", serverAddr, 2*time.Second)
	require.NoError(err)
	defer conn.Close()

	// Send a real ClientHello.
	clientHello := captureClientHello(t, "github.com")
	_, err = conn.Write(clientHello)
	require.NoError(err)

	proxy := &TLSProxy{
		matcher:    matcher,
		lookupHost: net.DefaultResolver.LookupHost,
		logger:     log.Noop,
	}

	buf, blocked := proxy.peekAndCheckServerCert(conn, "some-bypass.sslip.io", "127.0.0.1:12345")
	assert.True(t, blocked, "should block connection when server cert contains denied domain")
	assert.NotEmpty(t, buf, "should have buffered server bytes")
}
