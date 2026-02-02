# Web Previews

Expose ports from the sandbox to your Tailnet to preview web apps running inside the agent's environment.

## How it works

1. Run a web server in the sandbox (e.g., `npm run dev` on port 3000)
2. Expose the port
3. Get a preview URL accessible from your Tailnet

```
Your Device → Tailscale (MagicDNS) → Sandbox Pod → Your App
```

## Usage

**iOS App:** Previews tab → + → enter port → Expose

**Agent:** Can auto-expose ports when it detects a server starting.

Preview URL format: `http://sandbox-<session-id>.YOUR-TAILNET.ts.net:<port>`

## Notes

- **Tailnet only** - URLs only work from devices on your Tailnet
- **HTTP only** - no HTTPS (Tailnet traffic is already encrypted by WireGuard)
- **Multiple ports** - same hostname, different ports
- **Not persistent** - URLs change on pause/resume

## How it works internally

Control-plane creates a Kubernetes Service with `tailscale.com/expose: "true"`. Tailscale operator assigns MagicDNS hostname. NetworkPolicy updated to allow ingress from Tailscale CGNAT range (100.64.0.0/10).

## Troubleshooting

**Connection refused** - server might be listening on `127.0.0.1` instead of `0.0.0.0`. Use `--host 0.0.0.0` for Vite, `HOST=0.0.0.0` for CRA, etc.

**Slow first request** - Tailscale establishing direct connection. Subsequent requests are fast.
