package egress

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/slok/sbx/internal/model"
)

const (
	// peekSize is the number of bytes to peek for protocol classification.
	// TLS ClientHello + SNI typically fits in the first 512 bytes.
	peekSize = 1024
)

// Proxy is the egress TCP proxy. It accepts connections (redirected via nftables DNAT),
// classifies the protocol, checks the egress policy, and tunnels allowed connections.
type Proxy struct {
	listenAddr string
	matcher    *PolicyMatcher
	logger     Logger
	listener   net.Listener
}

// ProxyConfig is the configuration for the egress proxy.
type ProxyConfig struct {
	// ListenAddr is the address to listen on (e.g., "10.1.2.1:8443").
	ListenAddr string
	// Policy is the egress policy to enforce.
	Policy model.EgressPolicy
	// Logger for logging.
	Logger Logger
}

func (c *ProxyConfig) defaults() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.Logger == nil {
		c.Logger = noopLogger{}
	}
	return nil
}

// NewProxy creates a new egress TCP proxy.
func NewProxy(cfg ProxyConfig) (*Proxy, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Proxy{
		listenAddr: cfg.ListenAddr,
		matcher:    NewPolicyMatcher(cfg.Policy),
		logger:     cfg.Logger,
	}, nil
}

// ListenAndServe starts the proxy. Blocks until the listener is closed.
func (p *Proxy) ListenAndServe() error {
	ln, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.listenAddr, err)
	}
	p.listener = ln
	p.logger.Infof("Egress proxy listening on %s", p.listenAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Check if listener was closed (shutdown).
			if opErr, ok := err.(*net.OpError); ok && !opErr.Temporary() {
				return nil
			}
			p.logger.Warningf("Accept error: %v", err)
			continue
		}

		go p.handleConnection(conn)
	}
}

// Shutdown gracefully stops the proxy.
func (p *Proxy) Shutdown() error {
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

// handleConnection processes a single proxied connection.
func (p *Proxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// Get the original destination (before DNAT) via SO_ORIGINAL_DST.
	origDst, err := getOriginalDst(clientConn)
	if err != nil {
		// If we can't get original dst, we can't proxy.
		p.logger.Warningf("Could not get original destination: %v", err)
		return
	}

	origIP := origDst.IP
	origPort := origDst.Port

	// Peek at the first bytes to classify the protocol.
	buf := make([]byte, peekSize)
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := clientConn.Read(buf)
	_ = clientConn.SetReadDeadline(time.Time{}) // clear deadline
	if err != nil {
		p.logger.Debugf("Read error during classification from %s: %v", clientConn.RemoteAddr(), err)
		return
	}
	peeked := buf[:n]

	// Classify the connection.
	result := Classify(peeked)

	switch result.Protocol {
	case ProtoTLS, ProtoHTTP:
		if result.Host != "" {
			if !p.matcher.AllowDomain(result.Host) {
				p.logger.Infof("DENY %s %s → %s:%d", result.Protocol, result.Host, origIP, origPort)
				return
			}
			p.logger.Infof("ALLOW %s %s → %s:%d", result.Protocol, result.Host, origIP, origPort)
		} else {
			// TLS/HTTP but couldn't extract host — fall back to IP check.
			if !p.matcher.AllowIP(origIP) {
				p.logger.Infof("DENY %s (no host) → %s:%d", result.Protocol, origIP, origPort)
				return
			}
			p.logger.Infof("ALLOW %s (no host) → %s:%d", result.Protocol, origIP, origPort)
		}

	default:
		// Unknown protocol — only allow if IP matches a CIDR rule.
		if !p.matcher.AllowIP(origIP) {
			p.logger.Infof("DENY unknown-proto → %s:%d", origIP, origPort)
			return
		}
		p.logger.Infof("ALLOW unknown-proto → %s:%d", origIP, origPort)
	}

	// Connect to the real destination.
	dstAddr := fmt.Sprintf("%s:%d", origIP, origPort)
	dstConn, err := net.DialTimeout("tcp", dstAddr, 10*time.Second)
	if err != nil {
		p.logger.Warningf("Failed to connect to %s: %v", dstAddr, err)
		return
	}
	defer dstConn.Close()

	// Write the peeked data first (we already read it from the client).
	if _, err := dstConn.Write(peeked); err != nil {
		p.logger.Warningf("Failed to write peeked data to %s: %v", dstAddr, err)
		return
	}

	// Bidirectional tunnel.
	tunnel(clientConn, dstConn)
}

// tunnel copies data bidirectionally between two connections.
func tunnel(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copy := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		// When one direction is done, close the write side to signal the other.
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}

	go copy(a, b)
	go copy(b, a)

	wg.Wait()
}

// getOriginalDst retrieves the original destination address before nftables DNAT.
// Implemented in platform-specific files (origdst_linux.go).
