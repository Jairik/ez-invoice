#!/usr/bin/env bash
# Build and serve the ez-invoice web interface on localhost.
set -euo pipefail

cd "$(dirname "$0")"

PORT="${1:-9090}"
if [[ "$PORT" == *:* ]]; then
  URL="http://${PORT}"
else
  URL="http://127.0.0.1:${PORT}"
fi

if [ ! -x ./ez-invoice ] || [ "$(find cmd internal -name '*.go' -newer ./ez-invoice | wc -l)" -gt 0 ]; then
  echo "Building ez-invoice..."
  go build -o ./ez-invoice ./cmd/ez-invoice
fi

echo "ez-invoice web interface: ${URL}"
if command -v xdg-open >/dev/null 2>&1; then
  (xdg-open "${URL}" >/dev/null 2>&1 &)
fi

exec ./ez-invoice serve "${PORT}"
