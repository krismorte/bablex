#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
INFRA="$ROOT/infra"
BACKEND="$ROOT/backend"
BOOTSTRAP="$INFRA/bootstrap"
ZIPFILE="$INFRA/lambda.zip"

printf 'Building Go Lambda as linux/arm64...\n'
rm -f "$BOOTSTRAP" "$ZIPFILE"

if command -v docker >/dev/null 2>&1; then
  # Compile inside a Linux amd64 container so an Apple Silicon/Intel macOS host
  # cannot accidentally produce a Darwin or arm64 Lambda binary.
  docker run --rm --platform linux/arm64 \
    -v "$BACKEND:/src" \
    -v "$INFRA:/out" \
    -w /src \
    -e GOOS=linux \
    -e GOARCH=arm64 \
    -e CGO_ENABLED=0 \
    golang:1.24-bookworm \
    /bin/sh -c '/usr/local/go/bin/go version && /usr/local/go/bin/go mod download && /usr/local/go/bin/go build -tags lambda.norpc -trimpath -ldflags="-s -w" -o /out/bootstrap ./cmd/api'
else
  # Fallback for environments without Docker. GOOS/GOARCH make the target explicit.
  cd "$BACKEND"
  command -v go >/dev/null 2>&1 || {
    echo "ERROR: Docker is unavailable and Go is not installed on this host." >&2
    exit 1
  }
  go mod download
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -trimpath -ldflags='-s -w' -o "$BOOTSTRAP" ./cmd/api
fi

chmod 0755 "$BOOTSTRAP"

# Verify the file itself before it ever reaches Lambda.
FILE_INFO="$(file "$BOOTSTRAP")"
printf '%s\n' "$FILE_INFO"
case "$FILE_INFO" in
  *"ELF 64-bit"*"ARM aarch64"*|*"ELF 64-bit"*"AMD ARM aarch64"*) ;;
  *)
    echo "ERROR: bootstrap is not an ELF ARM aarch64 Linux executable." >&2
    exit 1
    ;;
esac

# Verify the zipped bootstrap too, preventing an accidental stale/incorrect package.
(cd "$INFRA" && zip -j -X "$ZIPFILE" bootstrap >/dev/null)
TMP_BOOTSTRAP="$(mktemp)"
unzip -p "$ZIPFILE" bootstrap > "$TMP_BOOTSTRAP"
ZIP_FILE_INFO="$(file "$TMP_BOOTSTRAP")"
rm -f "$TMP_BOOTSTRAP"
printf 'Packaged: %s\n' "$ZIP_FILE_INFO"
case "$ZIP_FILE_INFO" in
  *"ELF 64-bit"*"ARM aarch64"*|*"ELF 64-bit"*"AMD ARM aarch64"*) ;;
  *)
    echo "ERROR: lambda.zip contains a non-ARM aarch64 bootstrap." >&2
    exit 1
    ;;
esac
rm -f "$BOOTSTRAP"

printf 'Building React frontend...\n'
cd "$ROOT/frontend"
npm install
npm run build

after_index="$ROOT/frontend/dist/index.html"
if [[ ! -f "$after_index" ]]; then
  echo "ERROR: frontend build did not produce frontend/dist/index.html" >&2
  exit 1
fi

printf 'Build complete. Lambda package is linux/arm64 and ready for Terraform.\n'
