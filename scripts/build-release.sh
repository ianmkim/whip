#!/usr/bin/env bash
# Cross-compile whip for the platforms we ship.
# Output: dist/whip-<os>-<arch>
set -euo pipefail

cd "$(dirname "$0")/.."

mkdir -p dist

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.version=${VERSION}"

targets=(
    "linux/amd64"
    "darwin/arm64"
)

for t in "${targets[@]}"; do
    os="${t%/*}"
    arch="${t#*/}"
    out="dist/whip-${os}-${arch}"
    echo "building ${out} (${VERSION})"
    GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
        go build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/whip
done

echo
ls -lh dist/
