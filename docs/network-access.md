# Network Access

Sandboxes run inside BoxLite microVMs. Network policy is applied at VM creation time via
[gvproxy](https://github.com/containers/gvisor-tap-vsock) — a userspace NAT stack that all
outbound VM traffic passes through.

## Default access

- **Full internet** — agents can clone repos, install packages, browse docs, call external APIs
- **LLM API secret substitution** — BoxLite MITM proxy injects real API keys in-flight for allowed provider endpoints; the agent only ever sees placeholder values
- **Control-plane** — always reachable (the host alias `host.boxlite.internal` is exempt from all filtering)
- **Tailnet** — accessible by default when the host is on Tailscale (see [Tailnet access](#tailnet-access))

## Secret substitution

API keys never enter the sandbox as real values. Instead:

1. The agent sees a placeholder env var: `ANTHROPIC_API_KEY=NETCLODE_PLACEHOLDER_anthropic`
2. BoxLite's MITM proxy intercepts outbound HTTPS to allowed provider hosts
3. The real key is substituted in the `Authorization`/`x-api-key` header before the request leaves the host

This applies to all supported providers (Anthropic, OpenAI, Mistral, GitHub Copilot, etc.). The
substitution is independent of network filtering — it works regardless of what `allow_net` is set to.

## Tailnet access

The control-plane container shares the Tailscale network namespace
(`network_mode: service:tailscale` in Docker Compose). This means all BoxLite VM traffic is
NATted through a container that has a live `tailscale0` interface.

As a result, **tailnet devices are reachable from sandboxes by default** when the host is on
Tailscale. The `--tailnet` flag signals intent and is persisted per session (survives pause/resume),
but it does not currently gate access at the network level.

```bash
netclode sessions create --repo owner/repo --tailnet
```

### What `--tailnet` does today

- Persists `tailnet_enabled = true` on the session in Redis
- Survives pause and resume — the flag is restored when the session boots again
- Logged at sandbox creation so operators can audit which sessions requested tailnet access
- Lays groundwork for enforcement once BoxLite exposes DNS zone configuration (see [Roadmap](#roadmap))

### Accepted gap

Direct IP access to `100.64.0.0/10` (Tailscale CGNAT) is not blocked when `--tailnet` is not
requested. This is a known limitation of BoxLite v0.8.2 — see [Roadmap](#roadmap).

The realistic risk for AI agents is low: agents work with hostnames, not raw IPs, and they have
no prior knowledge of Tailscale device IPs on your network.

## Network model comparison

This fork uses BoxLite instead of Kubernetes. The two runtimes have fundamentally different
network primitives.

| Feature | Upstream K8s | This fork (BoxLite) |
|---|---|---|
| **Internet access** | ✅ allowed (except private ranges) | ✅ allowed (full, via empty `allow_net`) |
| **Private IP blocking** (10/8, 172.16/12, 192.168/16) | ✅ CIDR NetworkPolicy | ❌ Docker bridge subnet and host LAN reachable via bridge gateway |
| **Redis reachable from sandbox** | ✅ blocked by NetworkPolicy | ❌ reachable via Docker bridge — same internal network |
| **Host LAN reachable** (192.168.x.x) | ✅ blocked by NetworkPolicy | ❌ reachable via Docker bridge gateway → host routing |
| **Tailnet blocked by default** | ✅ CIDR NetworkPolicy blocks 100.64.0.0/10 | ⚠️ reachable via host Tailscale namespace |
| **Tailnet opt-in (`--tailnet`)** | ✅ CIDR policy allows 100.64.0.0/10 | ✅ flag persisted, intent logged |
| **Secret substitution** | ✅ external secret-proxy + auth-proxy MITM | ✅ BoxLite gvproxy MITM (simpler, no proxy chain) |
| **Per-session network isolation** | ✅ per-pod NetworkPolicy | ✅ per-VM gvproxy instance |
| **DNS filtering** | ✅ CoreDNS-level (via NetworkPolicy egress) | ⚠️ no DNS sinkhole yet (deferred) |
| **Direct IP bypass prevention** | ✅ kernel-level (iptables/nftables) | ❌ not enforced |
| **Runtime** | Kubernetes / Kata Containers | BoxLite microVM (gvproxy NAT) |

**Why Docker-internal services are reachable:** BoxLite VM traffic is NATted by gvproxy, which
runs inside the control-plane container. That container shares its network namespace with the
tailscale container and sits on the same Docker bridge as Redis. The actual traffic path is:

```
BoxLite VM → gvproxy (inside control-plane container) → Docker bridge → Redis container
                                                       → Docker bridge gateway → host → LAN
```

Redis is not exposed on any host port, but it is reachable container-to-container on the bridge
subnet (e.g. `172.20.0.x`). A rogue agent that discovers the bridge subnet could read and write
session state directly, bypassing the control-plane entirely.

Similarly, the Docker bridge gateway is the host itself, which routes traffic to the host LAN
(`192.168.x.x`). Home router admin panels, NAS devices, and other local hosts are reachable.

**Key difference from K8s:** The K8s path used CIDR-based `NetworkPolicy` objects — real
kernel-level IP blocking that explicitly covered the Docker bridge range (`172.16.0.0/12`),
private ranges, and the Tailscale CGNAT. BoxLite's `allow_net` is a **DNS sinkhole + SNI-based
TCP filter** that only activates when the allowlist is non-empty. Because agents need full internet
access, `allow_net` is empty and no IP-level filtering is active.

## Roadmap

The remaining network isolation gaps depend on BoxLite exposing its internal gvproxy DNS zone
configuration through the public wire format (`BoxOptions`). This is an upstream feature request.

### When BoxLite exposes `dns_zones` in `BoxOptions`

Once available, we can implement tailnet isolation without CIDR support:

```
default session:
  allow_net = []          ← full internet
  dns_zones = [{ name: "ts.net.", default_ip: "0.0.0.0" }]
  ↳ *.ts.net hostnames resolve to 0.0.0.0 (unreachable)

--tailnet session:
  allow_net = []          ← full internet
  dns_zones = []          ← no sinkhole, *.ts.net resolves normally
  ↳ agents can reach Tailscale devices by MagicDNS hostname
```

This covers the realistic threat (agents accessing tailnet via hostname) without requiring CIDR
blocking. The remaining gap (direct IP via `100.64.x.x`) requires knowledge of device IPs that
agents don't have, and is acceptable for a personal self-hosted deployment.

### What would require forking BoxLite

Full CIDR-based blocking (equivalent to K8s NetworkPolicies) would require one of:

- Adding CIDR filtering to gvproxy's outbound connection handling in Rust
- Running each BoxLite VM's gvproxy in an isolated network namespace with custom iptables rules

Neither is currently planned. If this becomes a priority, a PR to the BoxLite upstream adding
`block_cidrs` to `BoxOptions` would be the right approach.

## Configuration

```bash
# Enable tailnet access for a session
netclode sessions create --repo owner/repo --tailnet

# TAILSCALE_TAILNET in .env (optional, for future tailnet-aware features)
TAILSCALE_TAILNET=example.ts.net
```

## Troubleshooting

**Agent can't access the internet** — verify `allow_net` is empty in control-plane logs:
```
BoxLite: creating box ... allowNet=[]
```
If `allowNet` is non-empty, the DNS sinkhole is active and unrelated domains will fail.

**Agent can't reach a tailnet device by hostname** — the DNS sinkhole feature is not yet
implemented (deferred pending BoxLite API). Tailnet devices should be reachable if the host is
on Tailscale, since the control-plane shares the tailscale network namespace.

**`--tailnet` flag not surviving session resume** — verify the session's `tailnet_enabled` field
is `true` in Redis. The flag was persisted starting with the `boxlite-network-isolation` change.
