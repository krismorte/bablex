#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"

"$ROOT/build.sh"
cd "$ROOT/infra"
AWS_PROFILE=personal terraform init -backend-config=backend.hcl
AWS_PROFILE=personal terraform apply
AWS_PROFILE=personal terraform output app_url
