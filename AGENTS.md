# Netclode

## Deployment

Server is configured in `.env` as `DEPLOY_HOST`.

### Rollout after CI

After pushing changes, CI builds Docker images. To deploy:

```bash
# Wait for CI to complete
gh run watch

# Rollout all deployments (control-plane, web)
./scripts/rollout.sh

# Or rollout specific deployment
./scripts/rollout.sh control-plane
```

### Manual kubectl access

```bash
ssh $DEPLOY_HOST
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl -n netclode get pods
```

## Architecture

- **control-plane**: API server, WebSocket handler, session manager
- **web**: React frontend
- **agent**: Runs inside sandboxes created by control-plane (not a separate deployment)
