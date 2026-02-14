package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/slok/sbx/internal/log"
)

// LookupHostFunc resolves a hostname to a list of IP address strings.
type LookupHostFunc func(ctx context.Context, host string) ([]string, error)

// TLSProxyConfig is the configuration for the transparent TLS proxy.
type TLSProxyConfig struct {
	ListenAddr  string
	Matcher     *RuleMatcher
	Logger      log.Logger
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	LookupHost  LookupHostFunc
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
	if c.LookupHost == nil {
		c.LookupHost = net.DefaultResolver.LookupHost
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
	lookupHost  LookupHostFunc
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
		lookupHost:  cfg.LookupHost,
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

	// Defense-in-depth: resolve the SNI domain to IP addresses and verify that
	// none of them belong to an explicitly denied domain. This prevents bypasses
	// like sslip.io where an attacker uses a domain that passes rule matching
	// but resolves to a blocked server's IP address.
	if t.isDeniedByIPOverlap(ctx, sni, clientConn.RemoteAddr().String()) {
		return
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

	// Defense-in-depth: peek at the server's TLS response to extract the certificate
	// and verify that its domains (SANs/CN) don't match any deny rules. This catches
	// SNI-based bypasses (e.g. sslip.io) where the attacker's domain resolves to a
	// blocked server's IP — the server's real certificate will reveal its identity.
	serverBuf, blocked := t.peekAndCheckServerCert(targetConn, sni, clientConn.RemoteAddr().String())
	if blocked {
		targetConn.Close()
		return
	}

	// Forward any buffered server bytes to the client.
	if len(serverBuf) > 0 {
		if _, err := clientConn.Write(serverBuf); err != nil {
			targetConn.Close()
			t.logger.Errorf("failed to write server bytes to client: %v", err)
			return
		}
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

// peekAndCheckServerCert reads TLS records from the server connection until it finds
// the Certificate handshake message. It extracts the server's certificate SANs and
// checks them against deny rules. Returns all bytes read (to be forwarded to the client)
// and whether the connection should be blocked.
//
// If the certificate can't be parsed or no Certificate message is found within a
// reasonable number of records, the connection is allowed (fail-open). The primary
// domain-based SNI check already ran before this point.
func (t *TLSProxy) peekAndCheckServerCert(serverConn net.Conn, sni string, remoteAddr string) (buffered []byte, blocked bool) {
	// Only check if there are explicit deny rules with domains we can match
	// against the server certificate. Without deny rules, there's nothing to check.
	if len(t.matcher.DeniedDomains()) == 0 {
		return nil, false
	}

	_ = serverConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() { _ = serverConn.SetReadDeadline(time.Time{}) }()

	// Read up to 10 TLS records to find the Certificate message.
	// The typical TLS 1.2 handshake sends: ServerHello, Certificate, ServerKeyExchange, ServerHelloDone.
	// TLS 1.3 encrypts the Certificate, so we can only inspect TLS 1.2 certs.
	for i := 0; i < 10; i++ {
		// Read TLS record header (5 bytes).
		header := make([]byte, 5)
		if _, err := io.ReadFull(serverConn, header); err != nil {
			return buffered, false // Can't read — fail open.
		}

		recordLen := int(header[3])<<8 | int(header[4])
		if recordLen > 16384 {
			buffered = append(buffered, header...)
			return buffered, false // Abnormal record — fail open.
		}

		body := make([]byte, recordLen)
		if _, err := io.ReadFull(serverConn, body); err != nil {
			buffered = append(buffered, header...)
			return buffered, false
		}

		record := append(header, body...)
		buffered = append(buffered, record...)

		contentType := header[0]

		// Content type 20 = ChangeCipherSpec, 23 = Application Data.
		// In TLS 1.3, the Certificate is encrypted (wrapped in Application Data
		// records after the ServerHello). Once we see either of these, the
		// certificate is not inspectable — stop reading and fall through.
		if contentType == 20 || contentType == 23 {
			return buffered, false
		}

		// Content type 22 = Handshake.
		if contentType != 22 {
			continue // Skip other record types (e.g. alerts).
		}

		// Parse handshake messages within this record.
		// Handshake message format: type (1) + length (3) + body.
		data := body
		for len(data) >= 4 {
			hsType := data[0]
			hsLen := int(data[1])<<16 | int(data[2])<<8 | int(data[3])

			if 4+hsLen > len(data) {
				break // Incomplete handshake message.
			}

			// Handshake type 11 = Certificate.
			if hsType == 11 {
				certDomains := extractCertDomains(data[4 : 4+hsLen])
				for _, certDomain := range certDomains {
					if t.matcher.Match(certDomain) == ActionDeny {
						t.logger.Infof("denied TLS connection: server certificate contains denied domain %q (SNI was %q) src=%s",
							certDomain, sni, remoteAddr)
						return buffered, true
					}
				}
				// Certificate checked and clean — no need to read more records.
				return buffered, false
			}

			data = data[4+hsLen:]
		}
	}

	// Didn't find a Certificate message (TLS 1.3 encrypts it) — fail open.
	// The IP overlap check (isDeniedByIPOverlap) provides additional coverage.
	return buffered, false
}

// extractCertDomains parses a TLS Certificate handshake message body and returns
// all domain names found in the leaf certificate's Subject CN and SANs.
func extractCertDomains(certMsg []byte) []string {
	// Certificate message format:
	//   certificates_length (3 bytes)
	//   certificate_list:
	//     certificate_length (3 bytes) + certificate_data (DER)
	//     ...
	if len(certMsg) < 3 {
		return nil
	}

	certsLen := int(certMsg[0])<<16 | int(certMsg[1])<<8 | int(certMsg[2])
	if certsLen+3 > len(certMsg) || certsLen < 3 {
		return nil
	}

	// Parse only the first (leaf) certificate.
	certs := certMsg[3 : 3+certsLen]
	if len(certs) < 3 {
		return nil
	}

	certLen := int(certs[0])<<16 | int(certs[1])<<8 | int(certs[2])
	if certLen+3 > len(certs) {
		return nil
	}

	cert, err := x509.ParseCertificate(certs[3 : 3+certLen])
	if err != nil {
		return nil
	}

	var domains []string
	// Collect SANs.
	for _, san := range cert.DNSNames {
		domains = append(domains, strings.ToLower(san))
	}
	// Also check the CN (some older certs only have CN).
	if cn := strings.ToLower(cert.Subject.CommonName); cn != "" {
		domains = append(domains, cn)
	}

	return domains
}

// isDeniedByIPOverlap resolves the SNI domain and all explicitly denied domains,
// then checks for IP overlap. If the SNI resolves to the same IP as any denied
// domain, the connection is blocked. This prevents attacks like sslip.io where
// an attacker crafts a domain that resolves to a blocked server's IP.
//
// Resolution failures are logged but don't block the connection (fail-open for
// DNS errors), since a strict fail-closed would break connectivity when DNS is
// flaky. The primary domain-based check already ran before this point.
func (t *TLSProxy) isDeniedByIPOverlap(ctx context.Context, sni string, remoteAddr string) bool {
	deniedDomains := t.matcher.DeniedDomains()
	if len(deniedDomains) == 0 {
		return false
	}

	// Resolve the SNI domain.
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sniIPs, err := t.lookupHost(resolveCtx, sni)
	if err != nil {
		// Can't resolve the SNI — it will fail at dial anyway. Don't block.
		t.logger.Debugf("failed to resolve SNI %q for IP overlap check: %v", sni, err)
		return false
	}

	// Build a set of the SNI's resolved IPs.
	sniIPSet := make(map[string]struct{}, len(sniIPs))
	for _, ip := range sniIPs {
		sniIPSet[ip] = struct{}{}
	}

	// Resolve each denied domain and check for overlap.
	for _, denied := range deniedDomains {
		deniedIPs, err := t.lookupHost(resolveCtx, denied)
		if err != nil {
			t.logger.Debugf("failed to resolve denied domain %q for IP overlap check: %v", denied, err)
			continue
		}
		for _, ip := range deniedIPs {
			if _, overlap := sniIPSet[ip]; overlap {
				t.logger.Infof("denied TLS connection: SNI %q resolves to %s which belongs to denied domain %q src=%s",
					sni, ip, denied, remoteAddr)
				return true
			}
		}
	}

	return false
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
