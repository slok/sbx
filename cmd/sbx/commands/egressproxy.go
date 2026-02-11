package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alecthomas/kingpin/v2"
	"github.com/oklog/run"

	"github.com/slok/sbx/internal/egress"
	"github.com/slok/sbx/internal/model"
)

// EgressProxyCommand runs the internal egress proxy.
// This is an internal command spawned by the Firecracker engine — not user-facing.
type EgressProxyCommand struct {
	Cmd     *kingpin.CmdClause
	rootCmd *RootCommand

	listenAddr  string
	dnsAddr     string
	upstreamDNS string
	policyJSON  string
}

// NewEgressProxyCommand returns the egress-proxy command.
func NewEgressProxyCommand(rootCmd *RootCommand, app *kingpin.Application) *EgressProxyCommand {
	c := &EgressProxyCommand{rootCmd: rootCmd}

	c.Cmd = app.Command("egress-proxy", "Run egress proxy (internal).").Hidden()
	c.Cmd.Flag("listen", "TCP proxy listen address.").Required().StringVar(&c.listenAddr)
	c.Cmd.Flag("dns", "DNS forwarder listen address.").Required().StringVar(&c.dnsAddr)
	c.Cmd.Flag("upstream-dns", "Upstream DNS resolver.").Default("8.8.8.8:53").StringVar(&c.upstreamDNS)
	c.Cmd.Flag("policy", "Egress policy as JSON string.").Required().StringVar(&c.policyJSON)

	return c
}

func (c EgressProxyCommand) Name() string { return c.Cmd.FullCommand() }

func (c EgressProxyCommand) Run(ctx context.Context) error {
	logger := c.rootCmd.Logger

	// Parse policy from JSON argument.
	var policy model.EgressPolicy
	if err := json.Unmarshal([]byte(c.policyJSON), &policy); err != nil {
		return fmt.Errorf("invalid policy JSON: %w", err)
	}

	if err := policy.Validate(); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	// Adapt log.Logger to egress.Logger.
	egressLogger := &logAdapter{logger: logger}

	// Create TCP proxy.
	proxy, err := egress.NewProxy(egress.ProxyConfig{
		ListenAddr: c.listenAddr,
		Policy:     policy,
		Logger:     egressLogger,
	})
	if err != nil {
		return fmt.Errorf("could not create proxy: %w", err)
	}

	// Create DNS forwarder.
	dnsForwarder, err := egress.NewDNSForwarder(egress.DNSForwarderConfig{
		ListenAddr: c.dnsAddr,
		Upstream:   c.upstreamDNS,
		Logger:     egressLogger,
	})
	if err != nil {
		return fmt.Errorf("could not create DNS forwarder: %w", err)
	}

	// Run both proxy and DNS forwarder using oklog/run for lifecycle management.
	var g run.Group

	// TCP proxy.
	{
		g.Add(
			func() error {
				return proxy.ListenAndServe()
			},
			func(_ error) {
				_ = proxy.Shutdown()
			},
		)
	}

	// DNS forwarder.
	{
		g.Add(
			func() error {
				return dnsForwarder.ListenAndServe()
			},
			func(_ error) {
				_ = dnsForwarder.Shutdown()
			},
		)
	}

	// Context cancellation (from parent signal handling).
	{
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		g.Add(
			func() error {
				<-ctx.Done()
				return ctx.Err()
			},
			func(_ error) {
				cancel()
			},
		)
	}

	logger.Infof("Egress proxy starting: tcp=%s dns=%s", c.listenAddr, c.dnsAddr)
	return g.Run()
}

// logAdapter adapts the internal log.Logger to egress.Logger.
type logAdapter struct {
	logger interface {
		Debugf(format string, args ...any)
		Infof(format string, args ...any)
		Warningf(format string, args ...any)
		Errorf(format string, args ...any)
	}
}

func (a *logAdapter) Debugf(format string, args ...any)   { a.logger.Debugf(format, args...) }
func (a *logAdapter) Infof(format string, args ...any)    { a.logger.Infof(format, args...) }
func (a *logAdapter) Warningf(format string, args ...any) { a.logger.Warningf(format, args...) }
func (a *logAdapter) Errorf(format string, args ...any)   { a.logger.Errorf(format, args...) }
