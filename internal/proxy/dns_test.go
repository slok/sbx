package proxy_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/proxy"
)

// startDNSProxy starts a DNS proxy on a random UDP port and returns the address and cancel func.
func startDNSProxy(t *testing.T, matcher *proxy.RuleMatcher, dnsClient proxy.DNSClient) (addr string, cancel func()) {
	t.Helper()

	// Get a random free UDP port.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()

	addr = fmt.Sprintf("127.0.0.1:%d", port)

	p, err := proxy.NewDNSProxy(proxy.DNSProxyConfig{
		ListenAddr: addr,
		Upstream:   "8.8.8.8:53", // Won't be used since we mock the client.
		Matcher:    matcher,
		Logger:     log.Noop,
		DNSClient:  dnsClient,
	})
	require.NoError(t, err)

	ctx, ctxCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Run(ctx)
		close(done)
	}()

	// Wait for the DNS proxy to be ready (UDP).
	waitForDNSPort(t, addr)

	cancel = func() {
		ctxCancel()
		<-done
	}

	return addr, cancel
}

func waitForDNSPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		// Send a simple DNS query to check if the server is ready.
		c := new(dns.Client)
		c.Timeout = 200 * time.Millisecond
		m := new(dns.Msg)
		m.SetQuestion("test.", dns.TypeA)
		_, _, err := c.Exchange(m, addr)
		if err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for DNS proxy at %s to be ready", addr)
}

// fakeDNSClient is a mock DNS client that returns a configurable response.
type fakeDNSClient struct {
	handler func(m *dns.Msg) (*dns.Msg, error)
}

func (f *fakeDNSClient) ExchangeContext(_ context.Context, m *dns.Msg, _ string) (*dns.Msg, time.Duration, error) {
	resp, err := f.handler(m)
	return resp, 0, err
}

// newFakeDNSClientA returns a fake client that answers A queries with the given IP.
func newFakeDNSClientA(ip string) *fakeDNSClient {
	return &fakeDNSClient{
		handler: func(m *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(m)
			if len(m.Question) > 0 && m.Question[0].Qtype == dns.TypeA {
				resp.Answer = append(resp.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   m.Question[0].Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    60,
					},
					A: net.ParseIP(ip),
				})
			}
			return resp, nil
		},
	}
}

// newFakeDNSClientError returns a fake client that always returns an error.
func newFakeDNSClientError() *fakeDNSClient {
	return &fakeDNSClient{
		handler: func(_ *dns.Msg) (*dns.Msg, error) {
			return nil, fmt.Errorf("upstream failure")
		},
	}
}

func dnsQuery(t *testing.T, addr, domain string, qtype uint16) *dns.Msg {
	t.Helper()

	c := new(dns.Client)
	c.Timeout = 2 * time.Second
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), qtype)

	resp, _, err := c.Exchange(m, addr)
	require.NoError(t, err)
	return resp
}

func TestDNSProxyAllowDeny(t *testing.T) {
	tests := map[string]struct {
		defaultPolicy proxy.Action
		rules         []proxy.Rule
		queryDomain   string
		expRcode      int
		expAnswers    bool
	}{
		"Default allow with no rules should forward query.": {
			defaultPolicy: proxy.ActionAllow,
			queryDomain:   "example.com",
			expRcode:      dns.RcodeSuccess,
			expAnswers:    true,
		},
		"Default deny with no rules should refuse query.": {
			defaultPolicy: proxy.ActionDeny,
			queryDomain:   "example.com",
			expRcode:      dns.RcodeRefused,
			expAnswers:    false,
		},
		"Allow rule should forward matching domain.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "allowed.example.com"},
			},
			queryDomain: "allowed.example.com",
			expRcode:    dns.RcodeSuccess,
			expAnswers:  true,
		},
		"Deny rule should refuse matching domain.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "blocked.example.com"},
			},
			queryDomain: "blocked.example.com",
			expRcode:    dns.RcodeRefused,
			expAnswers:  false,
		},
		"Wildcard allow should forward subdomains.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.example.com"},
			},
			queryDomain: "api.example.com",
			expRcode:    dns.RcodeSuccess,
			expAnswers:  true,
		},
		"Wildcard should not match bare domain.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.example.com"},
			},
			queryDomain: "example.com",
			expRcode:    dns.RcodeRefused,
			expAnswers:  false,
		},
		"Catch-all deny should refuse everything.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*"},
			},
			queryDomain: "anything.test",
			expRcode:    dns.RcodeRefused,
			expAnswers:  false,
		},
		"First match wins: allow before deny.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "api.example.com"},
				{Action: proxy.ActionDeny, Domain: "*.example.com"},
			},
			queryDomain: "api.example.com",
			expRcode:    dns.RcodeSuccess,
			expAnswers:  true,
		},
		"First match wins: deny before allow.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "api.example.com"},
				{Action: proxy.ActionAllow, Domain: "*.example.com"},
			},
			queryDomain: "api.example.com",
			expRcode:    dns.RcodeRefused,
			expAnswers:  false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			fakeClient := newFakeDNSClientA("93.184.216.34")
			addr, cancel := startDNSProxy(t, matcher, fakeClient)
			defer cancel()

			resp := dnsQuery(t, addr, test.queryDomain, dns.TypeA)

			assert.Equal(test.expRcode, resp.Rcode)

			if test.expAnswers {
				assert.NotEmpty(resp.Answer, "expected answers in response")
			} else {
				assert.Empty(resp.Answer, "expected no answers in response")
			}
		})
	}
}

func TestDNSProxyUpstreamError(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	matcher, err := proxy.NewRuleMatcher(proxy.ActionAllow, nil)
	require.NoError(err)

	fakeClient := newFakeDNSClientError()
	addr, cancel := startDNSProxy(t, matcher, fakeClient)
	defer cancel()

	resp := dnsQuery(t, addr, "example.com", dns.TypeA)

	// When upstream fails, the proxy should return SERVFAIL.
	assert.Equal(dns.RcodeServerFailure, resp.Rcode)
}

func TestDNSProxyAAAAQuery(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	matcher, err := proxy.NewRuleMatcher(proxy.ActionDeny, []proxy.Rule{
		{Action: proxy.ActionAllow, Domain: "allowed.test"},
	})
	require.NoError(err)

	fakeClient := &fakeDNSClient{
		handler: func(m *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(m)
			if len(m.Question) > 0 && m.Question[0].Qtype == dns.TypeAAAA {
				resp.Answer = append(resp.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   m.Question[0].Name,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    60,
					},
					AAAA: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"),
				})
			}
			return resp, nil
		},
	}

	addr, cancel := startDNSProxy(t, matcher, fakeClient)
	defer cancel()

	// Allowed domain AAAA query should forward.
	resp := dnsQuery(t, addr, "allowed.test", dns.TypeAAAA)
	assert.Equal(dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(resp.Answer)

	// Denied domain AAAA query should refuse.
	resp = dnsQuery(t, addr, "blocked.test", dns.TypeAAAA)
	assert.Equal(dns.RcodeRefused, resp.Rcode)
	assert.Empty(resp.Answer)
}

func TestDNSProxyCaseInsensitive(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	matcher, err := proxy.NewRuleMatcher(proxy.ActionDeny, []proxy.Rule{
		{Action: proxy.ActionAllow, Domain: "example.com"},
	})
	require.NoError(err)

	fakeClient := newFakeDNSClientA("93.184.216.34")
	addr, cancel := startDNSProxy(t, matcher, fakeClient)
	defer cancel()

	// DNS is case-insensitive. Query with mixed case should still match.
	resp := dnsQuery(t, addr, "Example.COM", dns.TypeA)
	assert.Equal(dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(resp.Answer)
}

func TestNewDNSProxyValidation(t *testing.T) {
	tests := map[string]struct {
		cfg    proxy.DNSProxyConfig
		expErr bool
	}{
		"Missing matcher should fail.": {
			cfg:    proxy.DNSProxyConfig{},
			expErr: true,
		},
		"Valid config should succeed.": {
			cfg: proxy.DNSProxyConfig{
				Matcher: func() *proxy.RuleMatcher {
					m, _ := proxy.NewRuleMatcher(proxy.ActionAllow, nil)
					return m
				}(),
			},
			expErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			_, err := proxy.NewDNSProxy(test.cfg)
			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}
