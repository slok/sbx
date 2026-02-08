package lib

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/app/forward"
)

// Forward establishes port forwarding from the local host to a running sandbox.
//
// This method blocks until the context is cancelled. Use [context.WithCancel]
// to control the forwarding lifetime:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	go func() {
//	    time.Sleep(10 * time.Second)
//	    cancel() // stop forwarding
//	}()
//	err := client.Forward(ctx, "my-sandbox", []lib.PortMapping{{LocalPort: 8080, RemotePort: 80}})
//
// The sandbox must be in [SandboxStatusRunning] state. For Firecracker
// sandboxes, forwarding uses SSH tunnels.
//
// Returns nil on context cancellation (normal shutdown), [ErrNotFound] if the
// sandbox does not exist, or [ErrNotValid] if the sandbox is not running or
// ports are empty.
func (c *Client) Forward(ctx context.Context, nameOrID string, ports []PortMapping) error {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := forward.NewService(forward.ServiceConfig{
		Engine:     eng,
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	err = svc.Run(ctx, forward.Request{
		NameOrID: nameOrID,
		Ports:    toInternalPortMappings(ports),
	})
	if err != nil {
		return mapError(err)
	}

	return nil
}
