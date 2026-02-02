# Network Access

Sandboxes use Kubernetes NetworkPolicies for network isolation.

## Default access

- **Internet** - can reach external services (needed for LLM APIs)
- **Control-plane** - can talk to Netclode control-plane
- **DNS** - can resolve domains
- **No private networks** - blocked from 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
- **No Tailnet** by default - blocked from 100.64.0.0/10

## Tailnet access

Enable with `--tailnet` to let the sandbox reach your Tailscale network:

```bash
netclode sessions create --repo owner/repo --tailnet
```

Useful for accessing internal APIs, databases, or package registries on your tailnet.

| Flags | Internet | Tailnet |
|-------|----------|---------|
| (default) | Allowed | Blocked |
| `--tailnet` | Allowed | Allowed |

## How it works

Each sandbox gets NetworkPolicies:

1. **Base policy** - DNS + control-plane access
2. **Internet access** - allows `0.0.0.0/0` except private ranges
3. **Tailnet access** (if `--tailnet`) - allows `100.64.0.0/10`

```yaml
# Internet access policy (always created)
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sess-<id>-internet-access
spec:
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 10.0.0.0/8
              - 172.16.0.0/12
              - 192.168.0.0/16
              - 100.64.0.0/10
```

```yaml
# Tailnet access policy (created with --tailnet)
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sess-<id>-tailnet-access
spec:
  egress:
    - to:
        - ipBlock:
            cidr: 100.64.0.0/10
```

NetworkPolicies are additive, so the tailnet policy adds to the default internet access.

## Troubleshooting

```bash
# List policies for a session
kubectl --context netclode -n netclode get networkpolicies | grep <session-id>
```

**Tailnet not accessible with `--tailnet`** - verify the `sess-<id>-tailnet-access` NetworkPolicy exists.

**DNS not working** - check CoreDNS pods are running in kube-system.
