#!/usr/bin/env bash
set -euo pipefail

# Build a static Linux binary of poo for Harbor containers.
# CGO_ENABLED=0 ensures it runs on glibc (Debian/Ubuntu) and musl (Alpine) images alike.

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ARCH="${1:-$(go env GOARCH)}"
OUT="$ROOT/evals/harbor/bin/poo-linux-$ARCH"

mkdir -p "$(dirname "$OUT")"

echo "Building static linux/$ARCH binary -> $OUT"
(
  cd "$ROOT"
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -o "$OUT" ./cmd/poo
)
chmod +x "$OUT"
ls -lh "$OUT"
