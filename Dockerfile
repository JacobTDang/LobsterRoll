# syntax=docker/dockerfile:1
# Build any service:  docker build --build-arg SERVICE=watcher -t lobsterroll/watcher:dev .
# (Docker-free alternative for WSL: use `ko` — see docs/DEPLOY.md.)
FROM golang:1.25 AS build
WORKDIR /src
# go.mod AND go.sum first for a cached dependency layer.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE" || (echo "SERVICE build-arg is required" && exit 1)
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./services/${SERVICE}

# Static binary on a minimal nonroot base.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/app /usr/local/bin/app
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/app"]
