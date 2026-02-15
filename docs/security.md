# Security

## Overview

sbx sandboxes run as Firecracker microVMs — hardware-isolated virtual machines, not containers. Each sandbox gets its own kernel, network stack, and filesystem. The security model is built around:

1. **VM isolation** — Firecracker's KVM-based microVMs provide strong process and memory isolation
2. **Network control** — TAP devices with nftables rules control all traffic
3. **Egress filtering** — Domain-based allow/deny lists for outbound connections
4. **No host access** — Sandboxes cannot access the host filesystem or network directly

## Egress Filtering

Egress filtering controls what a sandbox can reach on the network. It is configured per-session via `sbx start -f session.yaml` or the SDK's `StartSandboxOpts.Egress`.

### How It Works

When an egress policy is set, sbx deploys three proxy components on the host:

| Proxy | Function |
|-------|----------|
| **HTTP proxy** | Intercepts HTTP/HTTPS CONNECT requests, checks domain against rules |
| **TLS/SNI proxy** | Transparent proxy that reads the TLS ClientHello SNI field to identify the domain. No MITM — connections are tunneled, not decrypted |
| **DNS proxy** | Resolves DNS queries, returns NXDOMAIN for denied domains |

Traffic from the sandbox is redirected through these proxies via nftables DNAT rules. The proxies run on the host, outside the sandbox.

### Configuration

Session YAML:

```yaml
egress:
  default: deny        # block everything by default
  rules:
    - { domain: "github.com", action: allow }
    - { domain: "*.github.com", action: allow }
    - { domain: "registry.npmjs.org", action: allow }
```

SDK:

```go
client.StartSandbox(ctx, "my-sandbox", &lib.StartSandboxOpts{
    Egress: &lib.EgressPolicy{
        DefaultAction: lib.EgressActionDeny,
        Rules: []lib.EgressRule{
            {Domain: "github.com", Action: lib.EgressActionAllow},
            {Domain: "*.github.com", Action: lib.EgressActionAllow},
        },
    },
})
```

### Patterns

**Deny all** — Fully offline sandbox:

```yaml
egress:
  default: deny
```

**Allowlist (recommended)** — Only allow specific services:

```yaml
egress:
  default: deny
  rules:
    - { domain: "github.com", action: allow }
    - { domain: "*.npmjs.org", action: allow }
    - { domain: "api.openai.com", action: allow }
```

**Denylist** — Allow everything except specific domains:

```yaml
egress:
  default: allow
  rules:
    - { domain: "evil.com", action: deny }
    - { domain: "*.malware.net", action: deny }
```

### What the Sandbox Can and Cannot Do

**Can:**
- Make outbound HTTP/HTTPS requests to allowed domains
- Resolve DNS for allowed domains
- Receive forwarded ports from the host (via `sbx forward`)

**Cannot:**
- Access the host filesystem
- Access host services (except through explicit port forwarding)
- Bypass egress rules (traffic is redirected at the nftables level)
- See other sandboxes' network traffic
- Inspect or modify TLS connections from the host side (no MITM)

### Egress is Per-Session

Egress policies are not persisted with the sandbox. Each `sbx start` applies its own session config. Restarting without `-f` gives the sandbox unrestricted network access.

This is by design: the same sandbox can run with different network profiles depending on the use case.

## Network Architecture

Each sandbox gets:
- A dedicated TAP device on the host
- A deterministic IP and MAC address (derived from sandbox ID hash)
- nftables rules for masquerade (outbound NAT) and forwarding
- Optional egress proxy chain (HTTP + TLS + DNS)

See [networking.md](networking.md) for the full networking architecture, including nftables rule details, TAP setup, SSH access, and debugging commands.

## SSH Access

sbx communicates with sandboxes over SSH:
- Ed25519 key pairs are auto-generated per sandbox
- Keys are stored alongside sandbox data
- `sbx exec`, `sbx shell`, `sbx cp`, and `sbx forward` all use SSH under the hood
- No passwords, no interactive authentication
