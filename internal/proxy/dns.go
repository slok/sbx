package proxy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/slok/sbx/internal/log"
)

// DNSProxyConfig is the configuration for the DNS proxy server.
type DNSProxyConfig struct {
	ListenAddr string
	Upstream   string
	Matcher    *RuleMatcher
	Logger     log.Logger
	DNSClient  DNSClient
}

func (c *DNSProxyConfig) defaults() error {
	if c.ListenAddr == "" {
		c.ListenAddr = ":9667"
	}
	if c.Upstream == "" {
		c.Upstream = "8.8.8.8:53"
	}
	if c.Matcher == nil {
		return fmt.Errorf("matcher is required")
	}
	if c.Logger == nil {
		c.Logger = log.Noop
	}
	if c.DNSClient == nil {
		c.DNSClient = &dns.Client{Timeout: 5 * time.Second}
	}
	return nil
}

// DNSClient is the interface for making DNS queries to upstream resolvers.
// This allows injection of a mock client for testing.
type DNSClient interface {
	ExchangeContext(ctx context.Context, m *dns.Msg, address string) (*dns.Msg, time.Duration, error)
}

// DNSProxy is a DNS proxy server that enforces domain-based network rules.
// It intercepts DNS queries, evaluates the queried domain against the rule
// matcher, and either forwards the query to an upstream resolver (allow) or
// returns a refused response (deny).
type DNSProxy struct {
	udpServer *dns.Server
	tcpServer *dns.Server
	upstream  string
	matcher   *RuleMatcher
	logger    log.Logger
	client    DNSClient
}

// NewDNSProxy creates a new DNS proxy server.
func NewDNSProxy(cfg DNSProxyConfig) (*DNSProxy, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid DNS proxy config: %w", err)
	}

	d := &DNSProxy{
		upstream: cfg.Upstream,
		matcher:  cfg.Matcher,
		logger:   cfg.Logger,
		client:   cfg.DNSClient,
	}

	mux := dns.NewServeMux()
	mux.HandleFunc(".", d.handleDNS)

	d.udpServer = &dns.Server{
		Addr:    cfg.ListenAddr,
		Net:     "udp",
		Handler: mux,
	}
	d.tcpServer = &dns.Server{
		Addr:    cfg.ListenAddr,
		Net:     "tcp",
		Handler: mux,
	}

	return d, nil
}

// Run starts the DNS proxy and blocks until ctx is cancelled. It performs
// a graceful shutdown when the context is done.
func (d *DNSProxy) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		d.logger.Infof("DNS proxy (UDP) listening on %s", d.udpServer.Addr)
		if err := d.udpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("DNS UDP server: %w", err)
		}
	}()

	go func() {
		d.logger.Infof("DNS proxy (TCP) listening on %s", d.tcpServer.Addr)
		if err := d.tcpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("DNS TCP server: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("DNS proxy server error: %w", err)
	case <-ctx.Done():
		d.logger.Infof("shutting down DNS proxy")
		_ = d.udpServer.Shutdown()
		_ = d.tcpServer.Shutdown()
		return nil
	}
}

// handleDNS is the DNS handler that evaluates queries against rules.
func (d *DNSProxy) handleDNS(w dns.ResponseWriter, r *dns.Msg) {
	if len(r.Question) == 0 {
		d.refuseDNS(w, r)
		return
	}

	q := r.Question[0]
	// DNS names have a trailing dot (FQDN). Strip it for our matcher.
	domain := strings.TrimSuffix(strings.ToLower(q.Name), ".")

	action := d.matcher.Match(domain)

	if action == ActionDeny {
		d.logger.WithValues(log.Kv{
			"action":   "deny",
			"protocol": "dns",
			"domain":   domain,
			"qtype":    dns.TypeToString[q.Qtype],
			"src":      w.RemoteAddr().String(),
			"reason":   "rule-match",
		}).Infof("denied request")
		d.refuseDNS(w, r)
		return
	}

	d.logger.WithValues(log.Kv{
		"action":   "allow",
		"protocol": "dns",
		"domain":   domain,
		"qtype":    dns.TypeToString[q.Qtype],
		"src":      w.RemoteAddr().String(),
	}).Infof("allowed request")
	d.forwardDNS(w, r, domain)
}

// forwardDNS forwards a DNS query to the upstream resolver and writes the response.
func (d *DNSProxy) forwardDNS(w dns.ResponseWriter, r *dns.Msg, domain string) {
	resp, _, err := d.client.ExchangeContext(context.Background(), r, d.upstream)
	if err != nil {
		d.logger.Errorf("failed to forward DNS query for %q to %s: %v", domain, d.upstream, err)
		d.serverFailDNS(w, r)
		return
	}

	resp.Id = r.Id
	if err := w.WriteMsg(resp); err != nil {
		d.logger.Errorf("failed to write DNS response for %q: %v", domain, err)
	}
}

// refuseDNS sends a REFUSED response for denied queries.
func (d *DNSProxy) refuseDNS(w dns.ResponseWriter, r *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetRcode(r, dns.RcodeRefused)
	if err := w.WriteMsg(resp); err != nil {
		d.logger.Errorf("failed to write REFUSED DNS response: %v", err)
	}
}

// serverFailDNS sends a SERVFAIL response when upstream forwarding fails.
func (d *DNSProxy) serverFailDNS(w dns.ResponseWriter, r *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetRcode(r, dns.RcodeServerFailure)
	if err := w.WriteMsg(resp); err != nil {
		d.logger.Errorf("failed to write SERVFAIL DNS response: %v", err)
	}
}
