#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
INFRA="$ROOT/infra"

if [[ ! -f "$INFRA/lambda.zip" ]]; then
  echo "ERROR: $INFRA/lambda.zip does not exist. Run ./build.sh first." >&2
  exit 1
fi

docker run --rm --platform linux/amd64 \
  -v "$INFRA:/out" \
  -w /out \
  golang:1.24-bookworm \
  /bin/sh -c '/usr/local/go/bin/go version >/dev/null && unzip -p /out/lambda.zip bootstrap > /tmp/bootstrap && file /tmp/bootstrap && case "$(file /tmp/bootstrap)" in *"ELF 64-bit"*"x86-64"*|*"ELF 64-bit"*"AMD x86-64"*) exit 0;; *) echo "ERROR: lambda.zip bootstrap is not linux/amd64." >&2; exit 1;; esac'
