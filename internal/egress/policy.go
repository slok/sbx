package egress

import (
	"net"
	"strings"

	"github.com/slok/sbx/internal/model"
)

// PolicyMatcher evaluates egress rules against domains and IPs.
type PolicyMatcher struct {
	defaultAction model.EgressAction
	rules         []model.EgressRule
	cidrNets      []*net.IPNet // pre-parsed CIDR networks, indexed same as rules.
}

// NewPolicyMatcher creates a policy matcher from an EgressPolicy.
func NewPolicyMatcher(policy model.EgressPolicy) *PolicyMatcher {
	cidrNets := make([]*net.IPNet, len(policy.Rules))
	for i, r := range policy.Rules {
		if r.CIDR != "" {
			_, ipNet, _ := net.ParseCIDR(r.CIDR) // already validated.
			cidrNets[i] = ipNet
		}
	}

	return &PolicyMatcher{
		defaultAction: policy.Default,
		rules:         policy.Rules,
		cidrNets:      cidrNets,
	}
}

// AllowDomain checks if a domain is allowed by the policy.
// Returns true if allowed.
func (m *PolicyMatcher) AllowDomain(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	for _, r := range m.rules {
		if r.Domain == "" {
			continue
		}
		if matchDomain(r.Domain, domain) {
			return r.Action == model.EgressActionAllow
		}
	}

	return m.defaultAction == model.EgressActionAllow
}

// AllowIP checks if an IP is allowed by CIDR rules in the policy.
// Returns true if allowed.
func (m *PolicyMatcher) AllowIP(ip net.IP) bool {
	for i, r := range m.rules {
		if m.cidrNets[i] == nil {
			continue
		}
		if m.cidrNets[i].Contains(ip) {
			return r.Action == model.EgressActionAllow
		}
	}

	return m.defaultAction == model.EgressActionAllow
}

// matchDomain matches a domain against a pattern.
// Supports exact match and wildcard prefix ("*.example.com").
// Wildcard matches any subdomain but not the base domain itself.
func matchDomain(pattern, domain string) bool {
	pattern = strings.ToLower(pattern)

	if strings.HasPrefix(pattern, "*.") {
		// Wildcard: *.example.com matches foo.example.com, bar.baz.example.com
		// but NOT example.com itself.
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(domain, suffix)
	}

	return pattern == domain
}
