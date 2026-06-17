# syntax=docker/dockerfile:1
# Build any service: docker build --build-arg SERVICE=watcher -t lobsterroll/watcher .
FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE" || (echo "SERVICE build-arg is required" && exit 1)
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./services/${SERVICE}

FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/app /usr/local/bin/app
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/app"]
