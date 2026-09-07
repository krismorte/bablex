#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

echo "Checking source formatting..."
gofmt -d backend/cmd/api/main.go
if command -v terraform >/dev/null 2>&1; then
  echo "Validating Terraform syntax..."
  terraform -chdir=infra fmt -check -recursive
else
  echo "Terraform not installed; skipping Terraform validation."
fi

echo "Source checks complete. Full integration testing requires Docker."
