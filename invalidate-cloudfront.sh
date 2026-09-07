#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT/infra"

if ! command -v aws >/dev/null 2>&1; then
  echo "AWS CLI is required for CloudFront invalidation." >&2
  exit 127
fi

DIST_ID="$(AWS_PROFILE=personal terraform output -raw cloudfront_distribution_id)"
aws cloudfront create-invalidation --distribution-id "$DIST_ID" --paths '/*' --profile personal
