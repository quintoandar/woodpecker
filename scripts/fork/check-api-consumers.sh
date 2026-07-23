#!/usr/bin/env bash
# ponytail: scans a directory tree for v2 Woodpecker API patterns; O(files) walk.
set -euo pipefail

ROOT="${1:-.}"
FOUND=0

patterns=(
  '/api/builds'
  '/api/tasks[^/]'
  'CI_BUILD_'
  'CI_JOB_'
  'woodpecker/v2'
  'WOODPECKER_DEV_OAUTH_HOST'
)

echo "Scanning ${ROOT} for deprecated Woodpecker v2 patterns (Go/TS/Python only)..."
for p in "${patterns[@]}"; do
  if rg -l "$p" "$ROOT" \
    --type go --type py --type ts --type tsx \
    --glob '!**/node_modules/**' \
    --glob '!**/.git/**' \
    2>/dev/null; then
    echo "  ^ matched pattern: $p"
    FOUND=1
  fi
done

if [[ "$FOUND" -eq 0 ]]; then
  echo "OK: no v2-only patterns found."
  exit 0
fi

echo "Review matches above before upgrading to upstream v3.16."
exit 1
