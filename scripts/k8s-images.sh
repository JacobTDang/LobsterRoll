#!/usr/bin/env bash
# Build the 7 service images as lobsterroll/<svc>:dev and load them into the
# local cluster. Prefers a docker-free path (ko -> tarball -> k3s containerd),
# which is what works on WSL without Docker Desktop. Falls back to docker+k3d
# when those are available.
set -euo pipefail
cd "$(dirname "$0")/.."
SERVICES="leaderboard watcher enrichment strategy trader notifier consensus pricewatch"
TAG="${TAG:-dev}"

have() { command -v "$1" >/dev/null 2>&1; }

if have docker && docker info >/dev/null 2>&1; then
  echo ">> building with docker"
  for s in $SERVICES; do
    echo ">> docker build $s"
    docker build --build-arg SERVICE="$s" -t "lobsterroll/$s:$TAG" .
  done
  if have k3d; then
    k3d image import $(for s in $SERVICES; do echo "lobsterroll/$s:$TAG"; done) -c lobsterroll
  elif have k3s || [ -S /run/k3s/containerd/containerd.sock ]; then
    for s in $SERVICES; do docker save "lobsterroll/$s:$TAG" | sudo k3s ctr images import -; done
  else
    echo "!! built images but no k3d/k3s found to import into"; exit 1
  fi

elif have ko; then
  echo ">> building with ko (docker-free)"
  export KO_DOCKER_REPO=lobsterroll
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  for s in $SERVICES; do
    echo ">> ko build $s"
    ko build --base-import-paths --tags "$TAG" --push=false --tarball="$tmp/$s.tar" "./services/$s"
    sudo k3s ctr images import "$tmp/$s.tar"
  done

else
  echo "!! need either a working docker daemon or 'ko' installed (see docs/DEPLOY.md)"; exit 1
fi

echo ">> images ready (lobsterroll/<svc>:$TAG)"
