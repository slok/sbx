package egress

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/slok/sbx/internal/model"
)

func TestPolicyMatcherAllowDomain(t *testing.T) {
	tests := map[string]struct {
		policy model.EgressPolicy
		domain string
		expOk  bool
	}{
		"Default deny, no rules, should deny.": {
			policy: model.EgressPolicy{Default: model.EgressActionDeny},
			domain: "github.com",
			expOk:  false,
		},
		"Default allow, no rules, should allow.": {
			policy: model.EgressPolicy{Default: model.EgressActionAllow},
			domain: "evil.com",
			expOk:  true,
		},
		"Exact domain match should allow.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "github.com", Action: model.EgressActionAllow},
				},
			},
			domain: "github.com",
			expOk:  true,
		},
		"Exact domain match is case insensitive.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "GitHub.COM", Action: model.EgressActionAllow},
				},
			},
			domain: "github.com",
			expOk:  true,
		},
		"Non-matching domain falls to default deny.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "github.com", Action: model.EgressActionAllow},
				},
			},
			domain: "evil.com",
			expOk:  false,
		},
		"Wildcard matches subdomain.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "*.npmjs.org", Action: model.EgressActionAllow},
				},
			},
			domain: "registry.npmjs.org",
			expOk:  true,
		},
		"Wildcard matches deep subdomain.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "*.npmjs.org", Action: model.EgressActionAllow},
				},
			},
			domain: "a.b.c.npmjs.org",
			expOk:  true,
		},
		"Wildcard does not match base domain.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "*.npmjs.org", Action: model.EgressActionAllow},
				},
			},
			domain: "npmjs.org",
			expOk:  false,
		},
		"Trailing dot is normalized.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "github.com", Action: model.EgressActionAllow},
				},
			},
			domain: "github.com.",
			expOk:  true,
		},
		"First matching rule wins.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionAllow,
				Rules: []model.EgressRule{
					{Domain: "evil.com", Action: model.EgressActionDeny},
					{Domain: "*.com", Action: model.EgressActionAllow},
				},
			},
			domain: "evil.com",
			expOk:  false,
		},
		"CIDR rules are ignored for domain matching.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{CIDR: "10.0.0.0/8", Action: model.EgressActionAllow},
				},
			},
			domain: "github.com",
			expOk:  false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			m := NewPolicyMatcher(test.policy)
			got := m.AllowDomain(test.domain)
			assert.Equal(t, test.expOk, got)
		})
	}
}

func TestPolicyMatcherAllowIP(t *testing.T) {
	tests := map[string]struct {
		policy model.EgressPolicy
		ip     string
		expOk  bool
	}{
		"Default deny, no rules, should deny.": {
			policy: model.EgressPolicy{Default: model.EgressActionDeny},
			ip:     "1.2.3.4",
			expOk:  false,
		},
		"Default allow, no rules, should allow.": {
			policy: model.EgressPolicy{Default: model.EgressActionAllow},
			ip:     "1.2.3.4",
			expOk:  true,
		},
		"IP in allowed CIDR should allow.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{CIDR: "10.0.0.0/8", Action: model.EgressActionAllow},
				},
			},
			ip:    "10.1.2.3",
			expOk: true,
		},
		"IP not in CIDR falls to default.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{CIDR: "10.0.0.0/8", Action: model.EgressActionAllow},
				},
			},
			ip:    "192.168.1.1",
			expOk: false,
		},
		"CIDR deny rule should deny.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionAllow,
				Rules: []model.EgressRule{
					{CIDR: "192.168.0.0/16", Action: model.EgressActionDeny},
				},
			},
			ip:    "192.168.1.1",
			expOk: false,
		},
		"Domain rules are ignored for IP matching.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionDeny,
				Rules: []model.EgressRule{
					{Domain: "github.com", Action: model.EgressActionAllow},
				},
			},
			ip:    "140.82.121.3",
			expOk: false,
		},
		"First matching CIDR rule wins.": {
			policy: model.EgressPolicy{
				Default: model.EgressActionAllow,
				Rules: []model.EgressRule{
					{CIDR: "10.0.0.0/8", Action: model.EgressActionDeny},
					{CIDR: "10.1.0.0/16", Action: model.EgressActionAllow},
				},
			},
			ip:    "10.1.2.3",
			expOk: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			m := NewPolicyMatcher(test.policy)
			got := m.AllowIP(net.ParseIP(test.ip))
			assert.Equal(t, test.expOk, got)
		})
	}
}
