package egress

import (
	"fmt"
	"net"

	"github.com/miekg/dns"
)

// DNSForwarder is a UDP DNS forwarder that forwards all queries to an upstream resolver.
// It prevents DNS tunneling by ensuring DNS only goes to a controlled upstream,
// not to arbitrary attacker-controlled nameservers.
type DNSForwarder struct {
	listenAddr string
	upstream   string
	logger     Logger
	server     *dns.Server
}

// DNSForwarderConfig is the configuration for DNSForwarder.
type DNSForwarderConfig struct {
	// ListenAddr is the address to listen on (e.g., "10.1.2.1:53").
	ListenAddr string
	// Upstream is the upstream DNS resolver (e.g., "8.8.8.8:53").
	Upstream string
	// Logger for logging.
	Logger Logger
}

func (c *DNSForwarderConfig) defaults() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}
	if c.Upstream == "" {
		c.Upstream = "8.8.8.8:53"
	}
	// Ensure upstream has port.
	if _, _, err := net.SplitHostPort(c.Upstream); err != nil {
		c.Upstream = c.Upstream + ":53"
	}
	if c.Logger == nil {
		c.Logger = noopLogger{}
	}
	return nil
}

// NewDNSForwarder creates a new DNS forwarder.
func NewDNSForwarder(cfg DNSForwarderConfig) (*DNSForwarder, error) {
	if err := cfg.defaults(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &DNSForwarder{
		listenAddr: cfg.ListenAddr,
		upstream:   cfg.Upstream,
		logger:     cfg.Logger,
	}, nil
}

// ListenAndServe starts the DNS forwarder. Blocks until the server is shut down.
func (f *DNSForwarder) ListenAndServe() error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", f.handleQuery)

	f.server = &dns.Server{
		Addr:    f.listenAddr,
		Net:     "udp",
		Handler: mux,
	}

	f.logger.Infof("DNS forwarder listening on %s, upstream %s", f.listenAddr, f.upstream)
	return f.server.ListenAndServe()
}

// Shutdown gracefully stops the DNS forwarder.
func (f *DNSForwarder) Shutdown() error {
	if f.server != nil {
		return f.server.Shutdown()
	}
	return nil
}

// handleQuery forwards a DNS query to the upstream resolver.
func (f *DNSForwarder) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	client := &dns.Client{Net: "udp"}

	resp, _, err := client.Exchange(r, f.upstream)
	if err != nil {
		f.logger.Warningf("DNS forward failed for %s: %v", formatQuestion(r), err)
		// Return SERVFAIL.
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeServerFailure)
		_ = w.WriteMsg(m)
		return
	}

	f.logger.Debugf("DNS forward: %s → %s", formatQuestion(r), formatAnswer(resp))
	_ = w.WriteMsg(resp)
}

// formatQuestion returns a human-readable DNS question string.
func formatQuestion(m *dns.Msg) string {
	if len(m.Question) == 0 {
		return "<no question>"
	}
	q := m.Question[0]
	return fmt.Sprintf("%s %s", q.Name, dns.TypeToString[q.Qtype])
}

// formatAnswer returns a summary of DNS answers.
func formatAnswer(m *dns.Msg) string {
	if m == nil || len(m.Answer) == 0 {
		return "<no answer>"
	}

	var ips []string
	for _, rr := range m.Answer {
		switch v := rr.(type) {
		case *dns.A:
			ips = append(ips, v.A.String())
		case *dns.AAAA:
			ips = append(ips, v.AAAA.String())
		case *dns.CNAME:
			ips = append(ips, "CNAME:"+v.Target)
		}
	}

	if len(ips) == 0 {
		return fmt.Sprintf("rcode=%s", dns.RcodeToString[m.Rcode])
	}
	return fmt.Sprintf("[%s]", joinStrings(ips, ", "))
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
