SERVICES := leaderboard watcher enrichment strategy trader notifier
REGISTRY  ?= ghcr.io/jacobtdang
TAG       ?= dev
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: all proto build test test-race vet lint tidy docker buildx k3d-up k3d-down deploy clean

all: test build

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

deploy:
	kubectl apply -k deploy/k8s

clean:
	rm -rf bin gen
