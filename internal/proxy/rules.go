package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Action represents what to do when a rule matches.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

// Rule defines a network policy rule with a domain pattern and action.
type Rule struct {
	Action Action `json:"action"`
	Domain string `json:"domain"`
}

// ParseRule parses a JSON string into a Rule.
func ParseRule(raw string) (Rule, error) {
	var r Rule
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return Rule{}, fmt.Errorf("invalid rule JSON: %w", err)
	}

	if r.Action != ActionAllow && r.Action != ActionDeny {
		return Rule{}, fmt.Errorf("invalid action %q: must be \"allow\" or \"deny\"", r.Action)
	}

	if r.Domain == "" {
		return Rule{}, fmt.Errorf("domain is required")
	}

	return r, nil
}

// RuleMatcher evaluates domains against an ordered list of rules.
// Rules are evaluated in order, first match wins. If no rule matches,
// the default policy is applied.
type RuleMatcher struct {
	rules         []Rule
	defaultPolicy Action
}

// NewRuleMatcher creates a new RuleMatcher with the given default policy and rules.
func NewRuleMatcher(defaultPolicy Action, rules []Rule) (*RuleMatcher, error) {
	if defaultPolicy != ActionAllow && defaultPolicy != ActionDeny {
		return nil, fmt.Errorf("invalid default policy %q: must be \"allow\" or \"deny\"", defaultPolicy)
	}

	return &RuleMatcher{
		rules:         rules,
		defaultPolicy: defaultPolicy,
	}, nil
}

// Match evaluates the domain against rules in order and returns the action.
// First matching rule wins. If no rule matches, returns the default policy.
func (m *RuleMatcher) Match(domain string) Action {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")

	for _, r := range m.rules {
		if matchDomain(r.Domain, domain) {
			return r.Action
		}
	}

	return m.defaultPolicy
}

// DefaultPolicy returns the default policy of the matcher.
func (m *RuleMatcher) DefaultPolicy() Action {
	return m.defaultPolicy
}

// DeniedDomains returns all explicit (non-wildcard) deny-action domains
// from the rule list. Wildcard patterns (e.g. "*.github.com") are excluded
// because they don't resolve to a single set of IPs.
func (m *RuleMatcher) DeniedDomains() []string {
	var domains []string
	for _, r := range m.rules {
		if r.Action == ActionDeny && !strings.HasPrefix(r.Domain, "*.") && r.Domain != "*" {
			domains = append(domains, strings.ToLower(r.Domain))
		}
	}
	return domains
}

// matchDomain checks if a domain matches a pattern.
//
// Matching rules:
//   - "github.com" matches exactly "github.com"
//   - "*.github.com" matches "api.github.com", "a.b.github.com" but NOT "github.com"
//   - "*" matches everything
func matchDomain(pattern, domain string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))

	if pattern == "*" {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		// Wildcard: *.github.com matches any subdomain of github.com.
		suffix := pattern[1:] // ".github.com"
		return strings.HasSuffix(domain, suffix)
	}

	// Exact match.
	return pattern == domain
}
