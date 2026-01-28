# Web Previews (Port Exposure)

Netclode allows you to expose ports from the sandbox to your Tailnet, enabling you to preview web applications running inside the agent's environment.

## Overview

When you're developing a web app in Netclode, you can expose ports to access the running application from any device on your Tailnet. This is useful for:

- Previewing frontend applications
- Testing APIs
- Debugging web services
- Sharing previews with team members on your Tailnet

## How It Works

1. The agent (or you) runs a web server on a port (e.g., `npm run dev` on port 3000)
2. You request to expose that port
3. Netclode creates a Tailscale service for the sandbox
4. You receive a preview URL accessible from your Tailnet

```
Your Device ──► Tailscale ──► Sandbox Pod ──► Your App
               (MagicDNS)      (port 3000)
```

## Usage

### iOS App

1. Open your session
2. Go to the **Previews** tab
3. Tap the **+** button
4. Enter the port number (e.g., 3000)
5. Tap **Expose**

The preview URL appears in the list. Tap to copy or open in Safari.

### Via Agent

The agent can also expose ports automatically when it detects a server starting. You'll see a `port_exposed` event in the activity feed.

### CLI (API)

Use the `expose_port` message in the Connect protocol:

```json
{
  "expose_port": {
    "session_id": "sess-abc123",
    "port": 3000
  }
}
```

Response:

```json
{
  "port_exposed": {
    "session_id": "sess-abc123",
    "port": 3000,
    "preview_url": "http://sandbox-abc123.tailnet-name.ts.net:3000"
  }
}
```

## Preview URL Format

Preview URLs follow this pattern:

```
http://sandbox-<session-id>.YOUR-TAILNET.ts.net:<port>
```

For example:
- `http://sandbox-9f7c8e64.mynet.ts.net:3000` - React dev server
- `http://sandbox-9f7c8e64.mynet.ts.net:8080` - API server
- `http://sandbox-9f7c8e64.mynet.ts.net:5173` - Vite dev server

## Accessing Previews

### From Your Devices

Preview URLs are accessible from any device connected to your Tailnet:

- **Mac/iPhone/iPad** with Tailscale installed
- **Other computers** on your Tailnet
- **Team members** with access to your Tailnet

### Browser Requirements

- Use a browser that respects Tailscale routing (Safari, Chrome, Firefox)
- On iOS, use Safari or any browser with proper Tailscale integration

### HTTPS Not Supported

Preview URLs use HTTP, not HTTPS. This is because:
- The sandbox doesn't have TLS certificates
- Internal Tailnet traffic is already encrypted by WireGuard

If your app requires HTTPS (e.g., for WebRTC, Service Workers), you'll need to:
1. Set up a reverse proxy with your own domain
2. Or use the app's dev mode to bypass HTTPS requirements

## Multiple Ports

You can expose multiple ports from the same sandbox. Each port uses the same hostname:

```
http://sandbox-abc123.mynet.ts.net:3000  # Frontend
http://sandbox-abc123.mynet.ts.net:8080  # API
http://sandbox-abc123.mynet.ts.net:5432  # Database (if needed)
```

## Architecture

### Tailscale Service

When you expose a port, the control-plane:

1. Creates a Kubernetes Service with Tailscale annotations
2. The Tailscale operator assigns a MagicDNS hostname
3. Updates the NetworkPolicy to allow traffic on that port

```yaml
apiVersion: v1
kind: Service
metadata:
  name: sandbox-<session-id>
  annotations:
    tailscale.com/expose: "true"
spec:
  selector:
    agents.x-k8s.io/sandbox: sandbox-<session-id>
  ports:
    - port: 3000
      targetPort: 3000
```

### NetworkPolicy

By default, sandbox pods can't receive inbound traffic (except from the control-plane). Exposing a port adds an ingress rule:

```yaml
ingress:
  - ports:
    - port: 3000
    from:
      - ipBlock:
          cidr: 100.64.0.0/10  # Tailscale CGNAT range
```

## Limitations

### Tailnet Only

Preview URLs are only accessible from your Tailnet. They're not public internet URLs.

To share with someone outside your Tailnet:
- Add them to your Tailnet
- Or deploy to a public hosting service

### No Persistent URLs

Preview URLs are tied to the sandbox pod. They change when:
- Session is paused and resumed (new pod)
- Session is deleted

### Port Must Be Listening

The port must have a process listening on it. If you expose a port before starting your server, requests will fail until the server starts.

## Troubleshooting

### Preview URL not working

1. **Check the server is running**:
   ```bash
   # In terminal
   curl localhost:3000
   ```

2. **Check the port is exposed**:
   ```bash
   kubectl --context netclode -n netclode get svc | grep sandbox
   ```

3. **Check Tailscale connectivity**:
   ```bash
   tailscale ping sandbox-<session-id>
   ```

### Connection refused

The server inside the sandbox might be listening on `127.0.0.1` instead of `0.0.0.0`:

```bash
# Bad: only localhost
npm run dev -- --host 127.0.0.1

# Good: all interfaces
npm run dev -- --host 0.0.0.0
```

Common framework flags:
- **Vite**: `--host 0.0.0.0`
- **Next.js**: Binds to 0.0.0.0 by default
- **Create React App**: `HOST=0.0.0.0`
- **Python**: `--bind 0.0.0.0`
- **Go**: Listen on `:3000` not `localhost:3000`

### Slow initial connection

The first request may be slow because:
1. Tailscale needs to establish a direct connection
2. DNS propagation for MagicDNS

Subsequent requests should be fast.

## API Reference

### ExposePortRequest

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session to expose port for |
| `port` | int32 | Port number to expose |
| `request_id` | string | Optional request correlation ID |

### PortExposedResponse

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session ID |
| `port` | int32 | Exposed port number |
| `preview_url` | string | Full URL to access the port |
| `request_id` | string | Echoed request ID |
