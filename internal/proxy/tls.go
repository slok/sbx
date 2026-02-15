package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/slok/sbx/internal/log"
)

// TLSProxyConfig is the configuration for the transparent TLS proxy.
type TLSProxyConfig struct {
	ListenAddr  string
	Matcher     *RuleMatcher
	Logger      log.Logger
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (c *TLSProxyConfig) defaults() error {
	if c.ListenAddr == "" {
		c.ListenAddr = ":9668"
	}
	if c.Matcher == nil {
		return fmt.Errorf("matcher is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	if c.DialContext == nil {
		c.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	return nil
}

// TLSProxy is a transparent TLS proxy that reads the SNI from the ClientHello
// to determine the target domain, checks it against rules, and either tunnels
// the connection to the real destination or closes it.
//
// Unlike the HTTP CONNECT proxy, this works transparently: the client doesn't
// know it's talking to a proxy. The TLS handshake is forwarded unmodified
// to the real server — there is no MITM or certificate replacement.
type TLSProxy struct {
	listener    net.Listener
	matcher     *RuleMatcher
	logger      log.Logger
	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	listenAddr  string
}

// NewTLSProxy creates a new transparent TLS proxy.
func NewTLSProxy(cfg TLSProxyConfig) (*TLSProxy, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid TLS proxy config: %w", err)
	}

	return &TLSProxy{
		matcher:     cfg.Matcher,
		logger:      cfg.Logger,
		dialContext: cfg.DialContext,
		listenAddr:  cfg.ListenAddr,
	}, nil
}

// Run starts the TLS proxy and blocks until ctx is cancelled.
func (t *TLSProxy) Run(ctx context.Context) error {
	var err error
	t.listener, err = net.Listen("tcp", t.listenAddr)
	if err != nil {
		return fmt.Errorf("TLS proxy listen error: %w", err)
	}

	t.logger.Infof("TLS proxy listening on %s", t.listenAddr)

	// Close listener when context is cancelled.
	go func() {
		<-ctx.Done()
		t.listener.Close()
	}()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // Context cancelled, normal shutdown.
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			t.logger.Errorf("TLS proxy accept error: %v", err)
			continue
		}

		go t.handleConn(ctx, conn)
	}
}

// handleConn processes a single connection by peeking at the TLS ClientHello.
func (t *TLSProxy) handleConn(ctx context.Context, clientConn net.Conn) {
	defer clientConn.Close()

	// Set a read deadline for the ClientHello peek.
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Peek at the TLS ClientHello to extract the SNI.
	// We need to read without consuming, so we use a buffered wrapper.
	peeked, sni, err := peekClientHelloSNI(clientConn)
	if err != nil {
		t.logger.Warningf("failed to read TLS ClientHello from %s: %v", clientConn.RemoteAddr(), err)
		return
	}

	// Clear the read deadline for the tunnel phase.
	_ = clientConn.SetReadDeadline(time.Time{})

	// Normalize SNI: strip trailing dot (FQDN form) so that "github.com."
	// is treated identically to "github.com" for both rule matching and dialing.
	sni = strings.TrimSuffix(sni, ".")

	domain := ExtractDomain(sni)

	// Block connections with an IP address as SNI (or any SNI that doesn't yield
	// a domain name). Domain-based rules cannot be evaluated without a domain,
	// and allowing IPs would bypass all egress filtering. This mirrors the HTTP
	// proxy's behavior in proxy.go.
	if domain == "" {
		t.logger.Infof("denied TLS connection to IP/empty SNI sni=%q src=%s", sni, clientConn.RemoteAddr())
		return
	}

	action := t.matcher.Match(domain)

	if action == ActionDeny {
		t.logger.Infof("denied TLS connection domain=%q sni=%q src=%s", domain, sni, clientConn.RemoteAddr())
		return // Close connection — client sees a connection reset.
	}

	t.logger.Debugf("allowed TLS connection domain=%q sni=%q src=%s", domain, sni, clientConn.RemoteAddr())

	// Dial the real destination on port 443.
	targetAddr := net.JoinHostPort(sni, "443")
	targetConn, err := t.dialContext(ctx, "tcp", targetAddr)
	if err != nil {
		t.logger.Errorf("failed to dial target %s: %v", targetAddr, err)
		return
	}

	// Replay the peeked bytes to the target.
	if _, err := targetConn.Write(peeked); err != nil {
		targetConn.Close()
		t.logger.Errorf("failed to write peeked bytes to target %s: %v", targetAddr, err)
		return
	}

	// Bidirectional tunnel.
	t.tunnel(clientConn, targetConn)
}

// tunnel performs bidirectional data copy between two connections.
func (t *TLSProxy) tunnel(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyConn := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}

	go copyConn(target, client)
	go copyConn(client, target)

	wg.Wait()
	target.Close()
}

// peekClientHelloSNI reads the TLS ClientHello from a connection and extracts the SNI.
// It returns all the bytes read (to be replayed to the target) and the SNI value.
func peekClientHelloSNI(conn net.Conn) (peeked []byte, sni string, err error) {
	// TLS record header is 5 bytes: content type (1) + version (2) + length (2).
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, "", fmt.Errorf("reading TLS record header: %w", err)
	}

	// Content type 22 = handshake.
	if header[0] != 22 {
		return header, "", fmt.Errorf("not a TLS handshake record (type=%d)", header[0])
	}

	// Read the full handshake record.
	recordLen := int(header[3])<<8 | int(header[4])
	if recordLen > 16384 {
		return header, "", fmt.Errorf("TLS record too large: %d", recordLen)
	}

	record := make([]byte, recordLen)
	if _, err := io.ReadFull(conn, record); err != nil {
		return append(header, record...), "", fmt.Errorf("reading TLS record body: %w", err)
	}

	peeked = append(header, record...)

	// Use Go's TLS library to parse the ClientHello.
	sni = extractSNIFromClientHello(peeked)
	if sni == "" {
		return peeked, "", fmt.Errorf("no SNI found in ClientHello")
	}

	return peeked, sni, nil
}

// extractSNIFromClientHello uses Go's tls.Server with a GetConfigForClient callback
// to extract the SNI from a ClientHello message without completing the handshake.
func extractSNIFromClientHello(raw []byte) string {
	var sni string

	// Create a pipe: write the raw ClientHello into one end, let tls.Server read from the other.
	clientReader, clientWriter := net.Pipe()

	// Write the ClientHello in a goroutine (Pipe is synchronous).
	go func() {
		_, _ = clientWriter.Write(raw)
		// Close after a short delay to let the TLS server read.
		// We don't need the full handshake, just the ClientHello parsing.
		time.AfterFunc(100*time.Millisecond, func() {
			clientWriter.Close()
		})
	}()

	// Use tls.Server with a callback that captures the SNI.
	tlsConn := tls.Server(clientReader, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName
			return nil, fmt.Errorf("sni extracted") // Abort handshake.
		},
	})

	// The Handshake will fail (we abort it), but we'll have captured the SNI.
	_ = tlsConn.Handshake()
	tlsConn.Close()
	clientReader.Close()

	return sni
}
