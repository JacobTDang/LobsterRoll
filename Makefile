SERVICES := leaderboard watcher enrichment strategy trader notifier consensus
REGISTRY  ?= ghcr.io/jacobtdang
TAG       ?= dev
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: all proto build test test-race vet lint tidy docker buildx k3d-up k3d-down deploy clean run-local natsd inject-trade trader-keys verify-alerts verify-consensus tg-chatid images k8s-secrets redeploy k8s-status dry-run dry-stop dry-logs

all: test build

# Local dev: read + alert + approve pipeline (NO execution). Safe from the US.
run-local:
	bash scripts/run-local.sh

# Standalone in-process NATS (no docker): make natsd
natsd:
	go run ./tools/natsd

# Publish a synthetic trade to exercise the alert/approval path:
#   make inject-trade ARGS="-side sell -size 12 -price 0.42"
inject-trade:
	go run ./tools/injecttrade $(ARGS)

# Derive Polymarket L2 API creds from TRADER_PRIVATE_KEY (geofenced for US):
#   TRADER_PRIVATE_KEY=... make trader-keys
trader-keys:
	go run ./tools/deriveapikeys $(ARGS)

# Headless end-to-end alert check (no real Telegram/RPC/keys): asserts an alert
# reaches a mock Telegram. Exits non-zero on failure.
verify-alerts:
	bash scripts/verify-alerts.sh

# Headless consensus check: injects a 3-wallet cohort and asserts a 🔥 CONSENSUS alert.
verify-consensus:
	bash scripts/verify-consensus.sh

# All-day DRY RUN: detached read->alert pipeline (no trader). Alerts -> Telegram.
dry-run:
	bash scripts/dry-run.sh
dry-stop:
	bash scripts/dry-stop.sh
dry-logs:
	tail -n +1 -F .local/dry-*.log

# Print your chat id (message your bot first):
#   TELEGRAM_BOT_TOKEN=... make tg-chatid
tg-chatid:
	go run ./tools/tgchatid

tidy:
	go mod tidy

proto:
	buf generate

build:
	@mkdir -p bin
	@for s in $(SERVICES); do echo ">> building $$s"; CGO_ENABLED=0 go build -trimpath -o bin/$$s ./services/$$s; done

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# Single-service image: make docker SERVICE=watcher
docker:
	docker build --build-arg SERVICE=$(SERVICE) -t $(REGISTRY)/$(SERVICE):$(TAG) .

# Multi-arch build+push for the Pi (arm64) and local (amd64).
buildx:
	@for s in $(SERVICES); do echo ">> buildx $$s"; docker buildx build --platform $(PLATFORMS) --build-arg SERVICE=$$s -t $(REGISTRY)/$$s:$(TAG) --push .; done

k3d-up:
	k3d cluster create lobsterroll --config deploy/k3d/cluster.yaml

k3d-down:
	k3d cluster delete lobsterroll

# Build all 7 service images and load them into the local cluster (docker-free
# via ko on WSL; docker+k3d if available). See docs/DEPLOY.md.
images:
	bash scripts/k8s-images.sh

# Apply your filled-in Secret (deploy/k8s/secrets/secret.yaml, gitignored).
k8s-secrets:
	kubectl apply -f deploy/k8s/secrets/secret.yaml

# Apply all manifests (run `make k8s-secrets` first).
deploy:
	kubectl apply -k deploy/k8s

# Build images + (re)deploy + restart so pods pick up new images.
redeploy: images deploy
	kubectl -n lobsterroll rollout restart deploy

k8s-status:
	kubectl -n lobsterroll get pods,svc,pvc

clean:
	rm -rf bin gen
