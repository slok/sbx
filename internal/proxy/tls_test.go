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
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			matcher, err := NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			// Start a TLS target server.
			targetCert := generateSelfSignedCert(t, test.sni)
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

			tlsProxy, err := NewTLSProxy(TLSProxyConfig{
				ListenAddr: proxyAddr,
				Matcher:    matcher,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%s", targetPort))
				},
			})
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
