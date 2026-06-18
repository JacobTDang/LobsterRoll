# Deploy on WSL (k3s + ko + k9s)

Runs all services 24/7, self-healing, in a local Kubernetes cluster. This path is
**Docker-free** — it uses `k3s` (native containerd) and `ko` (builds Go images
without a Docker daemon), which is what works on WSL without Docker Desktop.

Topology: `nats` + 7 services. `leaderboard` (:50051) and `enrichment` (:50052)
expose gRPC Services; `watcher`, `strategy`, `consensus`, `notifier` ride the bus;
`trader` ships at `replicas: 0` (gated — real money + US geofence).

## 1. One-time setup

**Enable systemd in WSL** (needed for the k3s service). In `/etc/wsl.conf`:
```ini
[boot]
systemd=true
```
Then from Windows: `wsl --shutdown`, and reopen the distro.

**Install k3s** (cluster + kubectl, no Docker):
```bash
curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml   # add to ~/.bashrc
kubectl get nodes                              # should show Ready
```

**Install ko** (image builder) and **k9s** (TUI) — Go is already installed:
```bash
go install github.com/google/ko@latest
go install github.com/derailed/k9s@latest
# ensure $(go env GOPATH)/bin is on PATH
```

## 2. Secrets

```bash
mkdir -p deploy/k8s/secrets
cp deploy/k8s/secret.example.yaml deploy/k8s/secrets/secret.yaml
# edit deploy/k8s/secrets/secret.yaml — at minimum, for the US-safe pipeline:
#   RPC_WSS_URL, TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID
```
`deploy/k8s/secrets/` is gitignored. Apply it:
```bash
kubectl create namespace lobsterroll   # or: kubectl apply -f deploy/k8s/namespace.yaml
make k8s-secrets
```

## 3. Build images + deploy

```bash
make images    # ko builds lobsterroll/<svc>:dev and imports into k3s containerd (needs sudo)
make deploy    # kubectl apply -k deploy/k8s
make k8s-status
```

`make redeploy` rebuilds images, re-applies, and restarts the deployments after a
code change.

## 4. Manage with k9s

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
k9s -n lobsterroll
```
In k9s: `:pods` to watch, `l` for logs, `d` to describe, `s` to shell. The pipeline
is healthy when `nats`, `leaderboard`, `enrichment`, `watcher`, `strategy`,
`consensus`, `notifier` are all `Running` (`trader` shows `0/0` — gated).

## 5. Turning on the trader (only when eligible)

Polymarket geofences US persons; leave the trader off unless you can legally trade.
When ready: fill `POLYMARKET_API_*` + `TRADER_PRIVATE_KEY` in the Secret, set
`TRADER_MAKER_ADDRESS`/`TRADER_FUNDER_ADDRESS` in the ConfigMap, then:
```bash
make k8s-secrets
kubectl -n lobsterroll scale deploy/trader --replicas=1
```

## Notes

- **SQLite single-writer:** `leaderboard`, `enrichment`, `watcher`, `consensus`,
  `trader` each own a `ReadWriteOnce` PVC and use `Recreate` — never scale them >1.
  `notifier` must also stay at 1 (Telegram long-poll offset is per-instance).
- **No `/healthz` yet:** only the gRPC services have (tcp) liveness probes; the
  bus-only services rely on crash-restart. A real health endpoint is Phase 9.
- **Docker alternative:** if you enable Docker Desktop's WSL integration,
  `make k3d-up` + `make images` (auto-detects docker) also works.
