package model

import (
	"fmt"
	"strconv"
	"strings"
)

// PortMapping represents a port forwarding configuration.
// LocalPort is the port on the host machine.
// RemotePort is the port inside the sandbox.
// BindAddress is the local address to listen on (e.g., "localhost", "0.0.0.0").
// Defaults to "localhost" if empty.
type PortMapping struct {
	BindAddress string
	LocalPort   int
	RemotePort  int
}

// ParsePortMapping parses a port mapping string.
// Supported formats:
//   - "8080" -> {LocalPort: 8080, RemotePort: 8080}
//   - "9000:8080" -> {LocalPort: 9000, RemotePort: 8080}
func ParsePortMapping(s string) (PortMapping, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PortMapping{}, fmt.Errorf("port mapping cannot be empty: %w", ErrNotValid)
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		// Short form: "8080" means same local and remote port
		port, err := parsePort(parts[0])
		if err != nil {
			return PortMapping{}, err
		}
		return PortMapping{LocalPort: port, RemotePort: port}, nil

	case 2:
		// Full form: "local:remote"
		localPort, err := parsePort(parts[0])
		if err != nil {
			return PortMapping{}, fmt.Errorf("invalid local port: %w", err)
		}
		remotePort, err := parsePort(parts[1])
		if err != nil {
			return PortMapping{}, fmt.Errorf("invalid remote port: %w", err)
		}
		return PortMapping{LocalPort: localPort, RemotePort: remotePort}, nil

	default:
		return PortMapping{}, fmt.Errorf("invalid port mapping format %q, expected 'port' or 'local:remote': %w", s, ErrNotValid)
	}
}

// parsePort parses and validates a single port number.
func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	port, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q: %w", s, ErrNotValid)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d out of range (1-65535): %w", port, ErrNotValid)
	}
	return port, nil
}

// String returns the string representation of the port mapping.
func (p PortMapping) String() string {
	if p.LocalPort == p.RemotePort {
		return strconv.Itoa(p.LocalPort)
	}
	return fmt.Sprintf("%d:%d", p.LocalPort, p.RemotePort)
}

// ListenAddress returns the bind address for display, defaulting to "localhost".
func (p PortMapping) ListenAddress() string {
	if p.BindAddress == "" {
		return "localhost"
	}
	return p.BindAddress
}
