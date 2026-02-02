# Operations

Day-to-day commands and troubleshooting.

## Accessing the cluster

Via kubectl (over Tailscale):

```bash
kubectl --context netclode -n netclode get pods
```

Via SSH:

```bash
ssh root@netclode
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl -n netclode get pods
```

## Logs

Control plane:

```bash
kubectl --context netclode -n netclode logs -l app=control-plane -f
```

Agent pods:

```bash
kubectl --context netclode -n netclode get pods -l sandbox=true
kubectl --context netclode -n netclode logs <agent-pod> -f
```

k3s:

```bash
ssh root@netclode journalctl -u k3s -f
```

## Managing pods

```bash
# List all
kubectl --context netclode get pods -A

# Describe
kubectl --context netclode -n netclode describe pod <pod-name>

# Exec
kubectl --context netclode -n netclode exec -it deploy/control-plane -- sh
kubectl --context netclode -n netclode exec -it <agent-pod> -- /bin/bash

# Restart
kubectl --context netclode -n netclode rollout restart deployment control-plane
```

## Sandboxes

```bash
# List
kubectl --context netclode -n netclode get sandbox
kubectl --context netclode -n netclode get sandboxclaim

# Describe
kubectl --context netclode -n netclode describe sandbox <name>

# Force delete stuck sandbox
kubectl --context netclode -n netclode delete sandbox <name> --force --grace-period=0

# Check warm pool
kubectl --context netclode -n netclode get sandboxwarmpool
```

## Storage

```bash
# PVCs
kubectl --context netclode -n netclode get pvc

# JuiceFS CSI
kubectl --context netclode -n kube-system get pods -l app=juicefs-csi-driver
kubectl --context netclode -n kube-system logs -l app=juicefs-csi-driver
```

For detailed JuiceFS maintenance procedures (garbage collection, trash cleanup, debugging), see [JuiceFS Maintenance Guide](../infra/docs/juicefs-maintenance.md).

## Networking

```bash
# Services
kubectl --context netclode -n netclode get svc

# Tailscale operator
kubectl --context netclode -n tailscale get pods
kubectl --context netclode -n tailscale logs -l app=operator
```

## Updating images

Images are built by CI on push to master.

```bash
# Wait for CI
gh run watch

# Rollout all
./scripts/rollout.sh

# Or specific
./scripts/rollout.sh control-plane
```

Manual trigger:

```bash
gh workflow run "Control Plane Image"
gh workflow run "Agent Image"
```

## Health checks

```bash
curl http://netclode/health
curl http://netclode/ready
```

## Resource usage

```bash
kubectl --context netclode top nodes
kubectl --context netclode -n netclode top pods
```

## Common issues

**Session stuck in "creating"** - sandbox VM failed to start:
```bash
kubectl --context netclode -n netclode describe sandbox sandbox-<session-id>
ssh root@netclode journalctl -u k3s | grep kata
```

**Agent not responding** - check process:
```bash
kubectl --context netclode -n netclode exec -it <agent-pod> -- ps aux | grep node
```

**Terminal not connecting** - check control plane logs:
```bash
kubectl --context netclode -n netclode logs -l app=control-plane | grep terminal
```

**Preview URLs not working** - check sandbox Tailscale service:
```bash
kubectl --context netclode -n netclode get svc | grep sandbox
```
