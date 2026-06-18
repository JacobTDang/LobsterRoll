#!/usr/bin/env bash
# Stop the dry-run pipeline started by scripts/dry-run.sh.
set -uo pipefail
cd "$(dirname "$0")/.."
PIDS=.local/dry.pids
if [ -f "$PIDS" ]; then
  while read -r pid; do [ -n "$pid" ] && kill "$pid" 2>/dev/null || true; done < "$PIDS"
  rm -f "$PIDS"
fi
# Belt-and-suspenders: free the dry-run ports.
for port in 4222 50051 50052; do fuser -k "${port}/tcp" 2>/dev/null || true; done
echo ">> dry run stopped"
