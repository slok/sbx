package firecracker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// createTAP creates a TAP device for the VM.
func (e *Engine) createTAP(tapDevice, gateway string) error {
	// Create TAP device
	if err := e.runCmd("ip", "tuntap", "add", "dev", tapDevice, "mode", "tap"); err != nil {
		// Ignore if already exists
		if !strings.Contains(err.Error(), "File exists") {
			return fmt.Errorf("failed to create TAP device %s: %w", tapDevice, err)
		}
		e.logger.Debugf("TAP device %s already exists", tapDevice)
	}

	// Assign IP address to TAP device (gateway address with /24)
	if err := e.runCmd("ip", "addr", "add", gateway+"/24", "dev", tapDevice); err != nil {
		// Ignore if already assigned
		if !strings.Contains(err.Error(), "File exists") {
			return fmt.Errorf("failed to assign IP to TAP device %s: %w", tapDevice, err)
		}
	}

	// Bring up TAP device
	if err := e.runCmd("ip", "link", "set", tapDevice, "up"); err != nil {
		return fmt.Errorf("failed to bring up TAP device %s: %w", tapDevice, err)
	}

	e.logger.Debugf("Created TAP device %s with gateway %s", tapDevice, gateway)
	return nil
}

// deleteTAP deletes a TAP device.
func (e *Engine) deleteTAP(tapDevice string) error {
	if err := e.runCmd("ip", "link", "del", tapDevice); err != nil {
		// Ignore if doesn't exist
		if strings.Contains(err.Error(), "Cannot find device") {
			return nil
		}
		return fmt.Errorf("failed to delete TAP device %s: %w", tapDevice, err)
	}
	e.logger.Debugf("Deleted TAP device %s", tapDevice)
	return nil
}

// setupIPTables sets up NAT and forwarding rules for the VM.
func (e *Engine) setupIPTables(tapDevice, gateway, vmIP string) error {
	// Get the default outbound interface
	outInterface, err := e.getDefaultInterface()
	if err != nil {
		return fmt.Errorf("failed to get default interface: %w", err)
	}

	subnet := e.subnetFromGateway(gateway)

	// NAT rule: masquerade outgoing traffic from VM subnet
	if err := e.runCmd("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", outInterface, "-s", subnet, "-j", "MASQUERADE"); err != nil {
		return fmt.Errorf("failed to add NAT rule: %w", err)
	}

	// Forward rule: allow traffic from TAP to outside
	if err := e.runCmd("iptables", "-A", "FORWARD", "-i", tapDevice, "-o", outInterface, "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("failed to add forward rule (out): %w", err)
	}

	// Forward rule: allow established/related traffic back to TAP
	if err := e.runCmd("iptables", "-A", "FORWARD", "-i", outInterface, "-o", tapDevice, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("failed to add forward rule (in): %w", err)
	}

	e.logger.Debugf("Set up iptables NAT for %s via %s", tapDevice, outInterface)
	return nil
}

// cleanupIPTables removes NAT and forwarding rules for the VM.
func (e *Engine) cleanupIPTables(tapDevice, gateway, vmIP string) error {
	outInterface, err := e.getDefaultInterface()
	if err != nil {
		// Can't cleanup if we don't know the interface
		e.logger.Warningf("Could not determine default interface for cleanup: %v", err)
		return nil
	}

	subnet := e.subnetFromGateway(gateway)

	// Remove NAT rule
	_ = e.runCmd("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", outInterface, "-s", subnet, "-j", "MASQUERADE")

	// Remove forward rules
	_ = e.runCmd("iptables", "-D", "FORWARD", "-i", tapDevice, "-o", outInterface, "-j", "ACCEPT")
	_ = e.runCmd("iptables", "-D", "FORWARD", "-i", outInterface, "-o", tapDevice, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")

	e.logger.Debugf("Cleaned up iptables rules for %s", tapDevice)
	return nil
}

// configureVMNetwork configures networking inside the VM via SSH.
// This sets up the IP address, default route, and DNS.
func (e *Engine) configureVMNetwork(ctx context.Context, vmIP, gateway string) error {
	sshKeyPath := e.sshKeyManager.PrivateKeyPath()

	// Wait for SSH to be available
	if err := e.waitForSSH(ctx, vmIP, sshKeyPath); err != nil {
		return fmt.Errorf("VM SSH not available: %w", err)
	}

	// Commands to configure network inside VM
	commands := []struct {
		name string
		args []string
	}{
		{
			name: "configure IP",
			args: []string{"ip", "addr", "add", vmIP + "/24", "dev", "eth0"},
		},
		{
			name: "bring up interface",
			args: []string{"ip", "link", "set", "eth0", "up"},
		},
		{
			name: "add default route",
			args: []string{"ip", "route", "add", "default", "via", gateway},
		},
		{
			name: "configure DNS",
			args: []string{"sh", "-c", "echo 'nameserver 8.8.8.8' > /etc/resolv.conf"},
		},
	}

	for _, cmd := range commands {
		if err := e.sshExec(ctx, vmIP, sshKeyPath, cmd.args); err != nil {
			// IP might already be configured, route might exist - continue on error
			e.logger.Warningf("SSH command '%s' failed (may be ok): %v", cmd.name, err)
		}
	}

	e.logger.Debugf("Configured network inside VM: %s", vmIP)
	return nil
}

// waitForSSH waits for SSH to become available on the VM.
func (e *Engine) waitForSSH(ctx context.Context, vmIP, sshKeyPath string) error {
	timeout := 60 * time.Second
	interval := 2 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Try to connect with a simple command
		if err := e.sshExec(ctx, vmIP, sshKeyPath, []string{"true"}); err == nil {
			return nil
		}

		time.Sleep(interval)
	}

	return fmt.Errorf("timeout waiting for SSH on %s", vmIP)
}

// sshExec executes a command on the VM via SSH.
func (e *Engine) sshExec(ctx context.Context, vmIP, sshKeyPath string, command []string) error {
	args := []string{
		"-i", sshKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
		fmt.Sprintf("root@%s", vmIP),
	}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh exec failed: %w, output: %s", err, string(output))
	}
	return nil
}

// getDefaultInterface returns the name of the default outbound network interface.
func (e *Engine) getDefaultInterface() (string, error) {
	// Get default route and extract interface name
	cmd := exec.Command("ip", "route", "show", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get default route: %w", err)
	}

	// Parse output: "default via 192.168.1.1 dev eth0 ..."
	fields := strings.Fields(string(output))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}

	return "", fmt.Errorf("could not parse default interface from: %s", string(output))
}

// subnetFromGateway converts gateway IP to subnet CIDR (e.g., 10.1.2.1 -> 10.1.2.0/24).
func (e *Engine) subnetFromGateway(gateway string) string {
	parts := strings.Split(gateway, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
	}
	return gateway + "/24"
}

// runCmd runs a command and returns an error if it fails.
func (e *Engine) runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w, output: %s", name, args, err, string(output))
	}
	return nil
}
