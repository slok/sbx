package proxy_test

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
	intproxy "github.com/slok/sbx/test/integration/proxy"
)

// These integration tests validate the proxy lifecycle using model.EgressPolicy,
// the same way the Firecracker engine spawns the proxy at sandbox start time.
// In PR B these will be changed to verify enforcement from inside the VM via sbx exec.

func TestEgressPolicyDenyDefault(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should-not-reach"))
	}))
	defer upstream.Close()

	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	client := newEgressProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEgressPolicyAllowDefault(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("allowed"))
	}))
	defer upstream.Close()

	egress := model.EgressPolicy{
		Default: model.EgressActionAllow,
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	client := newEgressProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "allowed", string(body))
}

func TestEgressPolicyDenyDefaultWithAllowRules(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("rule-allowed"))
	}))
	defer upstream.Close()

	// Default deny, but wildcard allow rule lets traffic through.
	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
		Rules: []model.EgressRule{
			{Domain: "*", Action: model.EgressActionAllow},
		},
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	client := newEgressProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "rule-allowed", string(body))
}

func TestEgressPolicyAllowDefaultWithDenyRules(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Default allow, but wildcard deny rule blocks traffic.
	egress := model.EgressPolicy{
		Default: model.EgressActionAllow,
		Rules: []model.EgressRule{
			{Domain: "*", Action: model.EgressActionDeny},
		},
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	client := newEgressProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEgressPolicyCONNECTAllow(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tls-egress"))
	}))
	defer upstream.Close()

	egress := model.EgressPolicy{
		Default: model.EgressActionAllow,
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "tls-egress", string(body))
}

func TestEgressPolicyCONNECTDeny(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(upstream.URL)
	if err != nil {
		// CONNECT failure may surface as a transport error.
		assert.Error(t, err)
	} else {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}
}

func TestEgressPolicyMultipleRulesFirstMatchWins(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("first-match"))
	}))
	defer upstream.Close()

	// Rules: deny *.blocked.test, allow *, deny * (should not be reached).
	// Default deny. The "allow *" should match the IP-based upstream before "deny *".
	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
		Rules: []model.EgressRule{
			{Domain: "*.blocked.test", Action: model.EgressActionDeny},
			{Domain: "*", Action: model.EgressActionAllow},
			{Domain: "*", Action: model.EgressActionDeny},
		},
	}

	proxyAddr, cancel := intproxy.StartProxyWithEgress(t, config, egress)
	defer cancel()

	client := newEgressProxyClient(proxyAddr)
	resp, err := client.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "first-match", string(body))
}

func TestEgressPolicyDNSDenyDefault(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
	}

	_, dnsAddr, cancel := intproxy.StartProxyWithEgressDNS(t, config, egress, upstreamAddr)
	defer cancel()

	resp := dnsQuery(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestEgressPolicyDNSAllowDefault(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	egress := model.EgressPolicy{
		Default: model.EgressActionAllow,
	}

	_, dnsAddr, cancel := intproxy.StartProxyWithEgressDNS(t, config, egress, upstreamAddr)
	defer cancel()

	resp := dnsQuery(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)
}

func TestEgressPolicyDNSAllowRuleOverridesDenyDefault(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
		Rules: []model.EgressRule{
			{Domain: "allowed.example.com", Action: model.EgressActionAllow},
		},
	}

	_, dnsAddr, cancel := intproxy.StartProxyWithEgressDNS(t, config, egress, upstreamAddr)
	defer cancel()

	// Allowed domain should be forwarded.
	resp := dnsQuery(t, dnsAddr, "allowed.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)

	// Non-matching domain should be refused.
	resp = dnsQuery(t, dnsAddr, "blocked.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func TestEgressPolicyDNSWildcardRule(t *testing.T) {
	config := intproxy.NewConfig(t)

	upstreamAddr, cleanupUpstream := startFakeDNSUpstream(t)
	defer cleanupUpstream()

	egress := model.EgressPolicy{
		Default: model.EgressActionDeny,
		Rules: []model.EgressRule{
			{Domain: "*.example.com", Action: model.EgressActionAllow},
		},
	}

	_, dnsAddr, cancel := intproxy.StartProxyWithEgressDNS(t, config, egress, upstreamAddr)
	defer cancel()

	// Subdomain matches wildcard, should be forwarded.
	resp := dnsQuery(t, dnsAddr, "api.example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.NotEmpty(t, resp.Answer)

	// Bare domain does NOT match *.example.com, should be refused.
	resp = dnsQuery(t, dnsAddr, "example.com", dns.TypeA)
	assert.Equal(t, dns.RcodeRefused, resp.Rcode)
	assert.Empty(t, resp.Answer)
}

func newEgressProxyClient(proxyAddr string) *http.Client {
	pURL, _ := url.Parse(fmt.Sprintf("http://%s", proxyAddr))
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(pURL),
		},
		Timeout: 5 * time.Second,
	}
}
