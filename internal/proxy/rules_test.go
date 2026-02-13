package proxy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/proxy"
)

func TestParseRule(t *testing.T) {
	tests := map[string]struct {
		raw     string
		expErr  bool
		expRule proxy.Rule
	}{
		"Valid allow rule should parse correctly.": {
			raw:     `{"action":"allow","domain":"*.github.com"}`,
			expRule: proxy.Rule{Action: proxy.ActionAllow, Domain: "*.github.com"},
		},
		"Valid deny rule should parse correctly.": {
			raw:     `{"action":"deny","domain":"evil.com"}`,
			expRule: proxy.Rule{Action: proxy.ActionDeny, Domain: "evil.com"},
		},
		"Invalid JSON should fail.": {
			raw:    `not json`,
			expErr: true,
		},
		"Invalid action should fail.": {
			raw:    `{"action":"block","domain":"foo.com"}`,
			expErr: true,
		},
		"Empty action should fail.": {
			raw:    `{"action":"","domain":"foo.com"}`,
			expErr: true,
		},
		"Empty domain should fail.": {
			raw:    `{"action":"allow","domain":""}`,
			expErr: true,
		},
		"Missing domain should fail.": {
			raw:    `{"action":"allow"}`,
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			rule, err := proxy.ParseRule(test.raw)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.Equal(test.expRule, rule)
			}
		})
	}
}

func TestNewRuleMatcher(t *testing.T) {
	tests := map[string]struct {
		defaultPolicy proxy.Action
		expErr        bool
	}{
		"Allow default policy should be valid.": {
			defaultPolicy: proxy.ActionAllow,
		},
		"Deny default policy should be valid.": {
			defaultPolicy: proxy.ActionDeny,
		},
		"Invalid default policy should fail.": {
			defaultPolicy: proxy.Action("invalid"),
			expErr:        true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			_, err := proxy.NewRuleMatcher(test.defaultPolicy, nil)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestRuleMatcherMatch(t *testing.T) {
	tests := map[string]struct {
		defaultPolicy proxy.Action
		rules         []proxy.Rule
		domain        string
		expAction     proxy.Action
	}{
		"No rules with allow default should allow.": {
			defaultPolicy: proxy.ActionAllow,
			domain:        "anything.com",
			expAction:     proxy.ActionAllow,
		},
		"No rules with deny default should deny.": {
			defaultPolicy: proxy.ActionDeny,
			domain:        "anything.com",
			expAction:     proxy.ActionDeny,
		},
		"Exact domain match should apply rule action.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "github.com"},
			},
			domain:    "github.com",
			expAction: proxy.ActionAllow,
		},
		"Exact domain match is case insensitive.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "GitHub.COM"},
			},
			domain:    "github.com",
			expAction: proxy.ActionAllow,
		},
		"Exact match should not match subdomains.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "github.com"},
			},
			domain:    "api.github.com",
			expAction: proxy.ActionDeny,
		},
		"Wildcard should match subdomains.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.github.com"},
			},
			domain:    "api.github.com",
			expAction: proxy.ActionAllow,
		},
		"Wildcard should match deep subdomains.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.github.com"},
			},
			domain:    "a.b.c.github.com",
			expAction: proxy.ActionAllow,
		},
		"Wildcard should NOT match bare domain.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.github.com"},
			},
			domain:    "github.com",
			expAction: proxy.ActionDeny,
		},
		"Wildcard should not match unrelated domains.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*.github.com"},
			},
			domain:    "gitlab.com",
			expAction: proxy.ActionAllow,
		},
		"Catch-all wildcard should match everything.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*"},
			},
			domain:    "anything.com",
			expAction: proxy.ActionDeny,
		},
		"First matching rule wins.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "*.github.com"},
				{Action: proxy.ActionDeny, Domain: "*"},
			},
			domain:    "api.github.com",
			expAction: proxy.ActionAllow,
		},
		"First matching rule wins even if later rule also matches.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*.evil.com"},
				{Action: proxy.ActionAllow, Domain: "*.evil.com"},
			},
			domain:    "www.evil.com",
			expAction: proxy.ActionDeny,
		},
		"Non-matching rules fall through to default.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "*.evil.com"},
				{Action: proxy.ActionDeny, Domain: "malware.org"},
			},
			domain:    "github.com",
			expAction: proxy.ActionAllow,
		},
		"Domain with whitespace is trimmed.": {
			defaultPolicy: proxy.ActionDeny,
			rules: []proxy.Rule{
				{Action: proxy.ActionAllow, Domain: "github.com"},
			},
			domain:    "  github.com  ",
			expAction: proxy.ActionAllow,
		},
		"Deny rule with exact match works.": {
			defaultPolicy: proxy.ActionAllow,
			rules: []proxy.Rule{
				{Action: proxy.ActionDeny, Domain: "evil.com"},
			},
			domain:    "evil.com",
			expAction: proxy.ActionDeny,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			matcher, err := proxy.NewRuleMatcher(test.defaultPolicy, test.rules)
			require.NoError(err)

			action := matcher.Match(test.domain)
			assert.Equal(test.expAction, action)
		})
	}
}
