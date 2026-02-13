package commands

import (
	"context"
	"fmt"

	"github.com/alecthomas/kingpin/v2"

	"github.com/slok/sbx/internal/proxy"
)

// ProxyCommand runs a standalone network proxy with domain-based rules.
type ProxyCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	port          int
	defaultPolicy string
	rules         []string
}

// NewProxyCommand returns the proxy command.
func NewProxyCommand(rootCmd *RootCommand, app *kingpin.Application) *ProxyCommand {
	c := &ProxyCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("internal-vm-proxy", "Internal: run a network proxy with domain-based rules.").Hidden()
	c.Cmd.Flag("port", "Port to listen on.").Default("9666").IntVar(&c.port)
	c.Cmd.Flag("default-policy", "Default policy when no rule matches.").Default("allow").EnumVar(&c.defaultPolicy, "allow", "deny")
	c.Cmd.Flag("rule", `Rule in JSON format (repeatable). E.g.: {"action":"allow","domain":"*.github.com"}`).StringsVar(&c.rules)

	return c
}

func (c ProxyCommand) Name() string { return c.Cmd.FullCommand() }

func (c ProxyCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Parse rules.
	rules := make([]proxy.Rule, 0, len(c.rules))
	for _, raw := range c.rules {
		r, err := proxy.ParseRule(raw)
		if err != nil {
			return fmt.Errorf("invalid rule %q: %w", raw, err)
		}
		rules = append(rules, r)
	}

	// Create matcher.
	matcher, err := proxy.NewRuleMatcher(proxy.Action(c.defaultPolicy), rules)
	if err != nil {
		return fmt.Errorf("could not create rule matcher: %w", err)
	}

	// Log configuration.
	logger.Infof("starting proxy on :%d with default policy %q (%d rules loaded)", c.port, c.defaultPolicy, len(rules))
	for i, r := range rules {
		logger.Infof("  rule[%d]: %s %s", i, r.Action, r.Domain)
	}

	// Create and run proxy.
	p, err := proxy.NewProxy(proxy.ProxyConfig{
		ListenAddr: fmt.Sprintf(":%d", c.port),
		Matcher:    matcher,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create proxy: %w", err)
	}

	return p.Run(ctx)
}
