package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/slok/sbx/internal/log"
)

// ProxyConfig is the configuration for the proxy server.
type ProxyConfig struct {
	ListenAddr  string
	Matcher     *RuleMatcher
	Logger      log.Logger
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (c *ProxyConfig) defaults() error {
	if c.ListenAddr == "" {
		c.ListenAddr = ":9666"
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

// Proxy is a forward HTTP proxy that enforces domain-based network rules.
// It handles both plain HTTP requests (via Host header) and HTTPS requests
// (via CONNECT method with SNI/target host inspection).
type Proxy struct {
	server      *http.Server
	matcher     *RuleMatcher
	logger      log.Logger
	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewProxy creates a new proxy server.
func NewProxy(cfg ProxyConfig) (*Proxy, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid proxy config: %w", err)
	}

	p := &Proxy{
		matcher:     cfg.Matcher,
		logger:      cfg.Logger,
		dialContext: cfg.DialContext,
	}

	p.server = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: p,
	}

	return p, nil
}

// Run starts the proxy and blocks until ctx is cancelled. It performs a
// graceful shutdown when the context is done.
func (p *Proxy) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		p.logger.Infof("proxy listening on %s", p.server.Addr)
		if err := p.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("proxy server error: %w", err)
	case <-ctx.Done():
		p.logger.Infof("shutting down proxy")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("proxy shutdown error: %w", err)
		}
		return nil
	}
}

// ServeHTTP dispatches requests to the appropriate handler based on method.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

// handleHTTP forwards plain HTTP requests after applying rules.
func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	domain := ExtractDomain(r.Host)

	if domain == "" {
		p.logger.Warningf("unidentifiable domain, applying rules then default policy (%s) src=%s method=%s url=%s",
			p.matcher.DefaultPolicy(), r.RemoteAddr, r.Method, r.URL.String())
	}

	action := p.matcher.Match(domain)
	if action == ActionDeny {
		p.logger.Infof("denied HTTP request domain=%q src=%s method=%s url=%s", domain, r.RemoteAddr, r.Method, r.URL.String())
		http.Error(w, fmt.Sprintf("blocked by proxy policy: %s", r.Host), http.StatusForbidden)
		return
	}

	p.logger.Debugf("allowed HTTP request domain=%q src=%s method=%s url=%s", domain, r.RemoteAddr, r.Method, r.URL.String())
	p.forwardHTTP(w, r)
}

// handleConnect handles HTTPS CONNECT tunneling after applying rules.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	domain := ExtractDomain(r.Host)

	if domain == "" {
		p.logger.Warningf("unidentifiable domain on CONNECT, applying rules then default policy (%s) src=%s target=%s",
			p.matcher.DefaultPolicy(), r.RemoteAddr, r.Host)
	}

	action := p.matcher.Match(domain)
	if action == ActionDeny {
		p.logger.Infof("denied CONNECT request domain=%q src=%s target=%s", domain, r.RemoteAddr, r.Host)
		http.Error(w, fmt.Sprintf("blocked by proxy policy: %s", r.Host), http.StatusForbidden)
		return
	}

	p.logger.Debugf("allowed CONNECT request domain=%q src=%s target=%s", domain, r.RemoteAddr, r.Host)

	// Dial the target.
	targetConn, err := p.dialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		p.logger.Errorf("failed to connect to target %s: %v", r.Host, err)
		http.Error(w, fmt.Sprintf("failed to connect to target: %v", err), http.StatusBadGateway)
		return
	}

	// Hijack the client connection.
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		targetConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		targetConn.Close()
		p.logger.Errorf("failed to hijack connection: %v", err)
		return
	}

	// Tell the client the tunnel is established.
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		clientConn.Close()
		targetConn.Close()
		p.logger.Errorf("failed to write CONNECT response: %v", err)
		return
	}

	// Bidirectional copy.
	p.tunnel(clientConn, targetConn)
}

// forwardHTTP forwards a plain HTTP request to the target and writes the response back.
func (p *Proxy) forwardHTTP(w http.ResponseWriter, r *http.Request) {
	// Ensure absolute URL for the outbound request.
	if r.URL.Scheme == "" {
		r.URL.Scheme = "http"
	}
	if r.URL.Host == "" {
		r.URL.Host = r.Host
	}

	// Remove hop-by-hop headers.
	removeHopByHopHeaders(r.Header)

	// Create a transport and execute the request.
	transport := &http.Transport{
		DialContext:           p.dialContext,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	resp, err := transport.RoundTrip(r)
	if err != nil {
		p.logger.Errorf("failed to forward request to %s: %v", r.URL.String(), err)
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers.
	removeHopByHopHeaders(resp.Header)
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// tunnel performs bidirectional data copy between two connections.
func (p *Proxy) tunnel(client, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyConn := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		// Signal the other side that we're done by closing write.
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}

	go copyConn(target, client)
	go copyConn(client, target)

	wg.Wait()
	client.Close()
	target.Close()
}

// ExtractDomain extracts the domain name from a host string, stripping
// the port if present. Returns empty string if the host is an IP address
// or cannot be determined.
func ExtractDomain(host string) string {
	// Strip port if present.
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		// No port, use as-is.
		h = host
	}

	h = strings.TrimSpace(h)

	// If it's an IP address, return empty (unidentifiable domain).
	if net.ParseIP(h) != nil {
		return ""
	}

	return strings.ToLower(h)
}

// hopByHopHeaders are headers that should not be forwarded by proxies.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopByHopHeaders(h http.Header) {
	for _, hdr := range hopByHopHeaders {
		h.Del(hdr)
	}
}
