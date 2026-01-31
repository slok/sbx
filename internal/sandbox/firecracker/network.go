package firecracker

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	// nftTableName is the name of the nftables table used by sbx.
	nftTableName = "sbx"
)

// createTAP creates a TAP device for the VM using netlink.
// This requires CAP_NET_ADMIN capability instead of root.
// The TAP device is owned by the current user so Firecracker can access it.
func (e *Engine) createTAP(tapDevice, gateway string) error {
	// Check if device already exists
	if link, err := netlink.LinkByName(tapDevice); err == nil {
		e.logger.Debugf("TAP device %s already exists", tapDevice)
		// Ensure it's up
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("failed to bring up existing TAP device %s: %w", tapDevice, err)
		}
		return nil
	}

	// Get current user's UID/GID so Firecracker can access the TAP device
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())

	// Create TAP device with current user as owner
	// This allows Firecracker (running as the same user) to open the device
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name: tapDevice,
		},
		Mode:  netlink.TUNTAP_MODE_TAP,
		Flags: netlink.TUNTAP_DEFAULTS | netlink.TUNTAP_NO_PI,
		Owner: uid,
		Group: gid,
	}

	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("failed to create TAP device %s: %w", tapDevice, err)
	}

	// Get the link after creation (needed for subsequent operations)
	link, err := netlink.LinkByName(tapDevice)
	if err != nil {
		return fmt.Errorf("failed to get TAP device %s after creation: %w", tapDevice, err)
	}

	// Parse gateway IP and create address with /24 mask
	gatewayIP := net.ParseIP(gateway)
	if gatewayIP == nil {
		return fmt.Errorf("invalid gateway IP: %s", gateway)
	}

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   gatewayIP,
			Mask: net.CIDRMask(24, 32),
		},
	}

	// Assign IP address to TAP device
	if err := netlink.AddrAdd(link, addr); err != nil {
		// Check if address already exists
		if !strings.Contains(err.Error(), "file exists") {
			return fmt.Errorf("failed to assign IP %s to TAP device %s: %w", gateway, tapDevice, err)
		}
	}

	// Bring up TAP device
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up TAP device %s: %w", tapDevice, err)
	}

	e.logger.Debugf("Created TAP device %s with gateway %s (owner uid=%d gid=%d)", tapDevice, gateway, uid, gid)
	return nil
}

// deleteTAP deletes a TAP device using netlink.
func (e *Engine) deleteTAP(tapDevice string) error {
	link, err := netlink.LinkByName(tapDevice)
	if err != nil {
		// Device doesn't exist, that's fine
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such") {
			return nil
		}
		return fmt.Errorf("failed to find TAP device %s: %w", tapDevice, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete TAP device %s: %w", tapDevice, err)
	}

	e.logger.Debugf("Deleted TAP device %s", tapDevice)
	return nil
}

// getDefaultInterface returns the name of the default outbound network interface using netlink.
func (e *Engine) getDefaultInterface() (string, error) {
	// Get all routes
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return "", fmt.Errorf("failed to list routes: %w", err)
	}

	// Find the default route
	// Default route can be identified by:
	// - Dst == nil, OR
	// - Dst.IP is all zeros (0.0.0.0/0)
	for _, route := range routes {
		isDefault := false
		if route.Dst == nil {
			isDefault = true
		} else if route.Dst.IP.Equal(net.IPv4zero) {
			isDefault = true
		}

		if isDefault && route.LinkIndex > 0 {
			// Get the link by index
			link, err := netlink.LinkByIndex(route.LinkIndex)
			if err != nil {
				continue
			}
			return link.Attrs().Name, nil
		}
	}

	return "", fmt.Errorf("no default route found")
}

// getInterfaceIndex returns the interface index for the given interface name.
func (e *Engine) getInterfaceIndex(ifaceName string) (uint32, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return 0, fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}
	return uint32(link.Attrs().Index), nil
}

// setupNftables sets up NAT and forwarding rules for the VM using nftables.
// This uses the google/nftables Go library which works with CAP_NET_ADMIN.
func (e *Engine) setupNftables(tapDevice, gateway, vmIP string) error {
	outInterface, err := e.getDefaultInterface()
	if err != nil {
		return fmt.Errorf("failed to get default interface: %w", err)
	}

	// Get interface indices
	outIfaceIdx, err := e.getInterfaceIndex(outInterface)
	if err != nil {
		return fmt.Errorf("failed to get output interface index: %w", err)
	}

	tapIfaceIdx, err := e.getInterfaceIndex(tapDevice)
	if err != nil {
		return fmt.Errorf("failed to get TAP interface index: %w", err)
	}

	// Parse subnet
	_, subnet, err := net.ParseCIDR(e.subnetFromGateway(gateway))
	if err != nil {
		return fmt.Errorf("failed to parse subnet: %w", err)
	}

	// Connect to nftables
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to connect to nftables: %w", err)
	}

	// Create or get our table
	table := &nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	}
	conn.AddTable(table)

	// Create NAT chain for postrouting (masquerade)
	natChain := &nftables.Chain{
		Name:     "postrouting",
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPostrouting,
		Priority: nftables.ChainPriorityNATSource,
	}
	conn.AddChain(natChain)

	// Create filter chain for forwarding
	filterChain := &nftables.Chain{
		Name:     "forward",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward,
		Priority: nftables.ChainPriorityFilter,
	}
	conn.AddChain(filterChain)

	// Rule 1: Masquerade traffic from VM subnet going out through the default interface
	// nft add rule ip sbx postrouting oifname "eno1" ip saddr 10.x.x.0/24 masquerade
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: natChain,
		Exprs: []expr.Any{
			// Match output interface
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ifname(outInterface),
			},
			// Match source IP in subnet
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseNetworkHeader,
				Offset:       12, // Source IP offset in IPv4 header
				Len:          4,
			},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           subnet.Mask,
				Xor:            []byte{0, 0, 0, 0},
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     subnet.IP.To4(),
			},
			// Masquerade
			&expr.Masq{},
		},
	})

	// Rule 2: Allow forwarding from TAP to outside
	// nft add rule ip sbx forward iif "sbx-xxx" oif "eno1" accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: filterChain,
		Exprs: []expr.Any{
			// Match input interface (TAP)
			&expr.Meta{Key: expr.MetaKeyIIF, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryUint32(tapIfaceIdx),
			},
			// Match output interface (default)
			&expr.Meta{Key: expr.MetaKeyOIF, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryUint32(outIfaceIdx),
			},
			// Accept
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// Rule 3: Allow established/related traffic back to TAP
	// nft add rule ip sbx forward iif "eno1" oif "sbx-xxx" ct state established,related accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: filterChain,
		Exprs: []expr.Any{
			// Match input interface (default)
			&expr.Meta{Key: expr.MetaKeyIIF, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryUint32(outIfaceIdx),
			},
			// Match output interface (TAP)
			&expr.Meta{Key: expr.MetaKeyOIF, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryUint32(tapIfaceIdx),
			},
			// Match connection state: established or related
			&expr.Ct{Register: 1, SourceRegister: false, Key: expr.CtKeySTATE},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryUint32(expr.CtStateBitESTABLISHED | expr.CtStateBitRELATED),
				Xor:            []byte{0, 0, 0, 0},
			},
			&expr.Cmp{
				Op:       expr.CmpOpNeq,
				Register: 1,
				Data:     []byte{0, 0, 0, 0},
			},
			// Accept
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// Commit the changes
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to apply nftables rules: %w", err)
	}

	e.logger.Debugf("Set up nftables NAT for %s via %s", tapDevice, outInterface)
	return nil
}

// cleanupNftables removes NAT and forwarding rules for the VM.
// For simplicity, we delete the entire sbx table if no other VMs are using it.
// In a production system, you'd want to track and remove individual rules.
func (e *Engine) cleanupNftables(tapDevice, gateway, vmIP string) error {
	conn, err := nftables.New()
	if err != nil {
		e.logger.Warningf("Failed to connect to nftables for cleanup: %v", err)
		return nil
	}

	// Get all tables and find ours
	tables, err := conn.ListTables()
	if err != nil {
		e.logger.Warningf("Failed to list nftables tables: %v", err)
		return nil
	}

	for _, table := range tables {
		if table.Name == nftTableName && table.Family == nftables.TableFamilyIPv4 {
			// For now, delete the entire table
			// TODO: In a multi-VM scenario, we should only delete specific rules
			conn.DelTable(table)
			if err := conn.Flush(); err != nil {
				e.logger.Warningf("Failed to delete nftables table: %v", err)
			} else {
				e.logger.Debugf("Cleaned up nftables table %s", nftTableName)
			}
			break
		}
	}

	return nil
}

// setupIPTables is a wrapper for backwards compatibility - now uses nftables.
func (e *Engine) setupIPTables(tapDevice, gateway, vmIP string) error {
	return e.setupNftables(tapDevice, gateway, vmIP)
}

func (e *Engine) cleanupIPTables(tapDevice, gateway, vmIP string) error {
	return e.cleanupNftables(tapDevice, gateway, vmIP)
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

// subnetFromGateway converts gateway IP to subnet CIDR (e.g., 10.1.2.1 -> 10.1.2.0/24).
func (e *Engine) subnetFromGateway(gateway string) string {
	parts := strings.Split(gateway, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
	}
	return gateway + "/24"
}

// Helper functions for nftables

// ifname returns the interface name as a null-terminated byte slice for nftables.
func ifname(name string) []byte {
	b := make([]byte, unix.IFNAMSIZ)
	copy(b, name)
	return b
}

// binaryUint32 converts a uint32 to a 4-byte slice in native endian.
func binaryUint32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}
