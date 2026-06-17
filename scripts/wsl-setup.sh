#!/usr/bin/env bash
# Dev toolchain bootstrap for WSL Ubuntu (no sudo required).
# Installs Go into $HOME/.local/go and the Go-based tools (buf, golangci-lint).
set -euo pipefail

GO_INSTALL_DIR="$HOME/.local"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

if ! command -v go >/dev/null 2>&1; then
  GO_VERSION="$(curl -fsSL https://go.dev/VERSION?m=text | head -1)"
  echo ">> installing ${GO_VERSION} (linux-${GOARCH}) into ${GO_INSTALL_DIR}/go"
  tarball="${GO_VERSION}.linux-${GOARCH}.tar.gz"
  curl -fsSL "https://go.dev/dl/${tarball}" -o "/tmp/${tarball}"
  mkdir -p "$GO_INSTALL_DIR"
  rm -rf "${GO_INSTALL_DIR}/go"
  tar -C "$GO_INSTALL_DIR" -xzf "/tmp/${tarball}"
  rm -f "/tmp/${tarball}"
else
  echo ">> go already present: $(command -v go)"
fi

# Ensure PATH for go + go-installed tools, idempotently.
PATH_LINE='export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"'
grep -qxF "$PATH_LINE" "$HOME/.bashrc" 2>/dev/null || echo "$PATH_LINE" >> "$HOME/.bashrc"
grep -qxF "$PATH_LINE" "$HOME/.profile" 2>/dev/null || echo "$PATH_LINE" >> "$HOME/.profile"
export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"

echo ">> $(go version)"
echo ">> installing buf + golangci-lint via 'go install'"
go install github.com/bufbuild/buf/cmd/buf@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

echo ">> done. Tools:"
command -v go buf golangci-lint
echo ">> open a new shell (or 'source ~/.bashrc') so PATH takes effect."
