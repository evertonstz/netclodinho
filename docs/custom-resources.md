# Custom VM Resources

Sessions run in Kata VMs with configurable CPU and memory.

## How it works

1. iOS app shows "Custom Resources" toggle in new session sheet
2. Select vCPUs (1/2/4/8) and memory (2/4/8/16 GB)
3. Control-plane validates against server limits
4. Kata VM gets full resources, K8s scheduler sees reduced requests (overcommit)

## Resource Limits

Limits are derived from host resources:

| Resource | Calculation | Example (16 CPU, 64GB host) |
|----------|-------------|----------------------------|
| Max vCPUs | 50% of host | 8 vCPUs |
| Max Memory | 25% of host, rounded to power of 2 | 16 GB |
| Default vCPUs | `DEFAULT_CPUS` env | 4 vCPUs |
| Default Memory | `DEFAULT_MEMORY_MB` env | 4096 MB |

## Overcommit

K8s requests are divided by overcommit ratio for scheduling, while Kata VMs get full resources:

```
K8s request = VM resources / overcommit_ratio
```

| Env Var | Default | Description |
|---------|---------|-------------|
| `CPU_OVERCOMMIT_RATIO` | 4 | CPU overcommit (4 = request 1/4 of actual) |
| `MEMORY_OVERCOMMIT_RATIO` | 4 | Memory overcommit |

Example with 4x overcommit:

| VM Gets | K8s Requests |
|---------|--------------|
| 8 vCPUs | 2 CPUs |
| 16 GB | 4 GB |

## Configuration

Set via environment variables on control-plane:

```bash
# View current config
kubectl --context netclode -n netclode exec deploy/control-plane -- env | grep -E "CPU|MEMORY|OVERCOMMIT"

# Update overcommit (triggers rollout)
kubectl --context netclode -n netclode set env deployment/control-plane \
  CPU_OVERCOMMIT_RATIO=4 \
  MEMORY_OVERCOMMIT_RATIO=4
```

## Bypassing Warm Pool

Sessions with custom resources (different from defaults) bypass the warm pool and create a dedicated sandbox. This is slower (~30s vs instant) but allows non-standard resource configurations.
