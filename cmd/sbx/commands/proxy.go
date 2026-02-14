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
	tlsPort       int
	dnsPort       int
	dnsUpstream   string
	defaultPolicy string
	rules         []string
}

// NewProxyCommand returns the proxy command.
func NewProxyCommand(rootCmd *RootCommand, app *kingpin.Application) *ProxyCommand {
	c := &ProxyCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("internal-vm-proxy", "Internal: run a network proxy with domain-based rules.").Hidden()
	c.Cmd.Flag("port", "Port to listen on for HTTP/HTTPS proxy.").Default("9666").IntVar(&c.port)
	c.Cmd.Flag("tls-port", "Port to listen on for transparent TLS proxy (0 to disable).").Default("0").IntVar(&c.tlsPort)
	c.Cmd.Flag("dns-port", "Port to listen on for DNS proxy (0 to disable).").Default("0").IntVar(&c.dnsPort)
	c.Cmd.Flag("dns-upstream", "Upstream DNS resolver address.").Default("8.8.8.8:53").StringVar(&c.dnsUpstream)
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

	// Create HTTP proxy.
	httpProxy, err := proxy.NewProxy(proxy.ProxyConfig{
		ListenAddr: fmt.Sprintf(":%d", c.port),
		Matcher:    matcher,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("could not create HTTP proxy: %w", err)
	}

	// Collect all proxies to run concurrently.
	type runnable struct {
		name string
		run  func(context.Context) error
	}
	var proxies []runnable
	proxies = append(proxies, runnable{name: "HTTP", run: httpProxy.Run})

	// Create TLS proxy if enabled.
	if c.tlsPort > 0 {
		logger.Infof("starting transparent TLS proxy on :%d", c.tlsPort)
		tlsProxy, err := proxy.NewTLSProxy(proxy.TLSProxyConfig{
			ListenAddr: fmt.Sprintf(":%d", c.tlsPort),
			Matcher:    matcher,
			Logger:     logger,
		})
		if err != nil {
			return fmt.Errorf("could not create TLS proxy: %w", err)
		}
		proxies = append(proxies, runnable{name: "TLS", run: tlsProxy.Run})
	}

	// Create DNS proxy if enabled.
	if c.dnsPort > 0 {
		logger.Infof("starting DNS proxy on :%d with upstream %s", c.dnsPort, c.dnsUpstream)
		dnsProxy, err := proxy.NewDNSProxy(proxy.DNSProxyConfig{
			ListenAddr: fmt.Sprintf(":%d", c.dnsPort),
			Upstream:   c.dnsUpstream,
			Matcher:    matcher,
			Logger:     logger,
		})
		if err != nil {
			return fmt.Errorf("could not create DNS proxy: %w", err)
		}
		proxies = append(proxies, runnable{name: "DNS", run: dnsProxy.Run})
	}

	// If only the HTTP proxy, run it directly.
	if len(proxies) == 1 {
		return proxies[0].run(ctx)
	}

	// Run all proxies concurrently. First error stops all.
	errCh := make(chan error, len(proxies))
	for _, p := range proxies {
		go func() { errCh <- p.run(ctx) }()
	}

	// Wait for first completion (error or context cancel).
	return <-errCh
}
