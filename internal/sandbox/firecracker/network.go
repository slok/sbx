package firecracker

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/slok/sbx/internal/ssh"
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

// setupNftables sets up NAT and forwarding rules for the VM using nftables.
// This uses the google/nftables Go library which works with CAP_NET_ADMIN.
//
// Docker compatibility: When Docker is installed, it creates a FORWARD chain with
// "policy drop" that blocks all forwarded traffic by default. Docker provides the
// DOCKER-USER chain specifically for user rules - packets go through DOCKER-USER
// before Docker's other rules. If DOCKER-USER exists, we add our forwarding rules
// there. Otherwise, we create our own forward chain in the sbx table.
func (e *Engine) setupNftables(tapDevice, gateway, vmIP string) error {
	outInterface, err := e.getDefaultInterface()
	if err != nil {
		return fmt.Errorf("failed to get default interface: %w", err)
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

	// Create our sbx table for NAT rules
	sbxTable := &nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	}
	conn.AddTable(sbxTable)

	// Create NAT chain for postrouting (masquerade)
	natChain := &nftables.Chain{
		Name:     "postrouting",
		Table:    sbxTable,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPostrouting,
		Priority: nftables.ChainPriorityNATSource,
	}
	conn.AddChain(natChain)

	// Rule: Masquerade traffic from VM subnet going out
	conn.AddRule(&nftables.Rule{
		Table: sbxTable,
		Chain: natChain,
		Exprs: []expr.Any{
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

	// Check if Docker's DOCKER-USER chain exists
	dockerUserChain := e.findDockerUserChain(conn)

	if dockerUserChain != nil {
		// Docker is present - add forwarding rules to DOCKER-USER chain
		// This is necessary because Docker's FORWARD chain has "policy drop"
		e.logger.Debugf("Found Docker's DOCKER-USER chain, adding forwarding rules there")

		// Rule: Allow forwarding from TAP
		conn.AddRule(&nftables.Rule{
			Table: dockerUserChain.Table,
			Chain: dockerUserChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifname(tapDevice),
				},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})

		// Rule: Allow forwarding to TAP (return traffic)
		conn.AddRule(&nftables.Rule{
			Table: dockerUserChain.Table,
			Chain: dockerUserChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifname(tapDevice),
				},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	} else {
		// No Docker - create our own forward chain in sbx table
		e.logger.Debugf("Docker's DOCKER-USER chain not found, creating own forward chain")

		filterChain := &nftables.Chain{
			Name:     "forward",
			Table:    sbxTable,
			Type:     nftables.ChainTypeFilter,
			Hooknum:  nftables.ChainHookForward,
			Priority: nftables.ChainPriorityFilter,
		}
		conn.AddChain(filterChain)

		// Rule: Allow forwarding from TAP
		conn.AddRule(&nftables.Rule{
			Table: sbxTable,
			Chain: filterChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifname(tapDevice),
				},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})

		// Rule: Allow forwarding to TAP (return traffic)
		conn.AddRule(&nftables.Rule{
			Table: sbxTable,
			Chain: filterChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifname(tapDevice),
				},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// Commit the changes
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to apply nftables rules: %w", err)
	}

	if dockerUserChain != nil {
		e.logger.Debugf("Set up nftables NAT for %s via %s (using DOCKER-USER)", tapDevice, outInterface)
	} else {
		e.logger.Debugf("Set up nftables NAT for %s via %s (standalone)", tapDevice, outInterface)
	}
	return nil
}

// findDockerUserChain looks for Docker's DOCKER-USER chain in the filter table.
// Returns nil if not found.
func (e *Engine) findDockerUserChain(conn *nftables.Conn) *nftables.Chain {
	chains, err := conn.ListChains()
	if err != nil {
		return nil
	}

	for _, chain := range chains {
		if chain.Name == "DOCKER-USER" &&
			chain.Table != nil &&
			chain.Table.Name == "filter" &&
			chain.Table.Family == nftables.TableFamilyIPv4 {
			return chain
		}
	}
	return nil
}

// cleanupNftables removes NAT and forwarding rules for the VM.
// This cleans up both our sbx table and any rules we added to Docker's DOCKER-USER chain.
// In a production system with multiple VMs, you'd want to track and remove individual rules.
func (e *Engine) cleanupNftables(tapDevice, gateway, vmIP string) error {
	conn, err := nftables.New()
	if err != nil {
		e.logger.Warningf("Failed to connect to nftables for cleanup: %v", err)
		return nil
	}

	// First, clean up any rules we added to Docker's DOCKER-USER chain
	e.cleanupDockerUserRules(conn, tapDevice)

	// Then delete our sbx table
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

// cleanupDockerUserRules removes any rules we added to Docker's DOCKER-USER chain.
func (e *Engine) cleanupDockerUserRules(conn *nftables.Conn, tapDevice string) {
	dockerUserChain := e.findDockerUserChain(conn)
	if dockerUserChain == nil {
		return // No DOCKER-USER chain, nothing to clean up
	}

	// Get all rules in DOCKER-USER chain
	rules, err := conn.GetRules(dockerUserChain.Table, dockerUserChain)
	if err != nil {
		e.logger.Warningf("Failed to get DOCKER-USER rules: %v", err)
		return
	}

	// Find and delete rules that reference our TAP device
	tapName := ifname(tapDevice)
	deletedCount := 0
	for _, rule := range rules {
		if ruleMatchesTapDevice(rule, tapName) {
			if err := conn.DelRule(rule); err != nil {
				e.logger.Warningf("Failed to delete DOCKER-USER rule: %v", err)
			} else {
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		if err := conn.Flush(); err != nil {
			e.logger.Warningf("Failed to flush DOCKER-USER cleanup: %v", err)
		} else {
			e.logger.Debugf("Cleaned up %d rules from DOCKER-USER for %s", deletedCount, tapDevice)
		}
	}
}

// ruleMatchesTapDevice checks if an nftables rule references the given TAP device name.
func ruleMatchesTapDevice(rule *nftables.Rule, tapName []byte) bool {
	for _, e := range rule.Exprs {
		if cmp, ok := e.(*expr.Cmp); ok {
			// Check if the comparison data matches our TAP device name
			if len(cmp.Data) == len(tapName) && string(cmp.Data) == string(tapName) {
				return true
			}
		}
	}
	return false
}

// setupProxyRedirect adds PREROUTING DNAT rules to redirect VM traffic through the proxy.
// TCP ports 80 and 443 are redirected to the proxy's HTTP port on the gateway IP.
// UDP port 53 is redirected to the proxy's DNS port on the gateway IP.
// This ensures all HTTP/HTTPS/DNS traffic from the VM is subject to egress filtering.
func (e *Engine) setupProxyRedirect(tapDevice, gateway, vmIP string, ports ProxyPorts) error {
	gatewayIP := net.ParseIP(gateway).To4()
	if gatewayIP == nil {
		return fmt.Errorf("invalid gateway IP: %s", gateway)
	}

	sourceIP := net.ParseIP(vmIP).To4()
	if sourceIP == nil {
		return fmt.Errorf("invalid VM IP: %s", vmIP)
	}

	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to connect to nftables: %w", err)
	}

	// Use the existing sbx table.
	sbxTable := &nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	}
	conn.AddTable(sbxTable)

	// Create PREROUTING chain for DNAT.
	preroutingChain := &nftables.Chain{
		Name:     "prerouting",
		Table:    sbxTable,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
	}
	conn.AddChain(preroutingChain)

	// Helper: build a DNAT rule for a specific protocol + destination port.
	// Matches: iifname <tap> && ip saddr <vmIP> && <proto> dport <origPort> → DNAT to <gateway>:<proxyPort>.
	addDNATRule := func(proto byte, origPort, proxyPort uint16) {
		// Protocol field offset in IPv4 header is 9, length 1.
		// For TCP/UDP, destination port is at transport header offset 2, length 2.
		conn.AddRule(&nftables.Rule{
			Table: sbxTable,
			Chain: preroutingChain,
			Exprs: []expr.Any{
				// Match input interface = TAP device.
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifname(tapDevice),
				},
				// Match source IP = VM IP.
				&expr.Payload{
					DestRegister: 1,
					Base:         expr.PayloadBaseNetworkHeader,
					Offset:       12, // Source IP offset.
					Len:          4,
				},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     sourceIP,
				},
				// Match protocol (TCP=6, UDP=17).
				&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     []byte{proto},
				},
				// Match destination port.
				&expr.Payload{
					DestRegister: 1,
					Base:         expr.PayloadBaseTransportHeader,
					Offset:       2, // Destination port offset.
					Len:          2,
				},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     binaryutil.BigEndian.PutUint16(origPort),
				},
				// DNAT to gateway:proxyPort.
				&expr.Immediate{
					Register: 1,
					Data:     gatewayIP,
				},
				&expr.Immediate{
					Register: 2,
					Data:     binaryutil.BigEndian.PutUint16(proxyPort),
				},
				&expr.NAT{
					Type:        expr.NATTypeDestNAT,
					Family:      unix.NFPROTO_IPV4,
					RegAddrMin:  1,
					RegProtoMin: 2,
				},
			},
		})
	}

	// Redirect HTTP (TCP 80) → proxy HTTP port.
	addDNATRule(unix.IPPROTO_TCP, 80, uint16(ports.HTTPPort))
	// Redirect HTTPS (TCP 443) → proxy HTTP port (proxy handles CONNECT).
	addDNATRule(unix.IPPROTO_TCP, 443, uint16(ports.HTTPPort))
	// Redirect DNS (UDP 53) → proxy DNS port.
	addDNATRule(unix.IPPROTO_UDP, 53, uint16(ports.DNSPort))

	if err := conn.Flush(); err != nil {
		return fmt.Errorf("failed to apply proxy redirect rules: %w", err)
	}

	e.logger.Debugf("Set up proxy DNAT redirect: %s TCP 80,443 -> %s:%d, UDP 53 -> %s:%d",
		vmIP, gateway, ports.HTTPPort, gateway, ports.DNSPort)
	return nil
}

// cleanupProxyRedirect removes the PREROUTING chain with proxy DNAT rules.
// This is called during Stop/Remove when egress filtering was active.
func (e *Engine) cleanupProxyRedirect() error {
	conn, err := nftables.New()
	if err != nil {
		e.logger.Warningf("Failed to connect to nftables for proxy redirect cleanup: %v", err)
		return nil
	}

	// Find the sbx table and its prerouting chain.
	tables, err := conn.ListTables()
	if err != nil {
		e.logger.Warningf("Failed to list nftables tables: %v", err)
		return nil
	}

	for _, table := range tables {
		if table.Name != nftTableName || table.Family != nftables.TableFamilyIPv4 {
			continue
		}

		chains, err := conn.ListChainsOfTableFamily(nftables.TableFamilyIPv4)
		if err != nil {
			e.logger.Warningf("Failed to list chains: %v", err)
			return nil
		}

		for _, chain := range chains {
			if chain.Table.Name == nftTableName && chain.Name == "prerouting" {
				conn.DelChain(chain)
				if err := conn.Flush(); err != nil {
					e.logger.Warningf("Failed to delete prerouting chain: %v", err)
				} else {
					e.logger.Debugf("Cleaned up proxy redirect prerouting chain")
				}
				return nil
			}
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

// sshExec executes a command on the VM via the Go SSH client.
func (e *Engine) sshExec(ctx context.Context, sandboxID string, command string) error {
	client, err := e.newSSHClientWithTimeout(ctx, sandboxID, 5*time.Second)
	if err != nil {
		return fmt.Errorf("ssh exec failed: %w", err)
	}
	defer client.Close()

	exitCode, err := client.Exec(ctx, command, ssh.ExecOpts{})
	if err != nil {
		return fmt.Errorf("ssh exec failed: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("ssh exec failed: command exited with code %d", exitCode)
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
