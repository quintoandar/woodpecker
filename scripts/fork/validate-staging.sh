#!/usr/bin/env bash
# ponytail: runs fork-critical unit tests; fails fast if GraphQL or RPC patches break.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

# upstream test helpers are behind the `test` build tag (see Makefile test-server)
GT=(go test -tags test -count=1)

echo "==> GraphQL config dir tests"
"${GT[@]}" ./server/forge/github/ -run 'TestDirGraphQL|TestGraphQLEndpoint'

echo "==> GitHub forge package"
"${GT[@]}" ./server/forge/github/

echo "==> RPC Log (no LastWork on log lines)"
"${GT[@]}" ./server/rpc/ -run 'Log'

echo "==> Hook handler (upstream async ack; no fork duplicate)"
"${GT[@]}" ./server/api/ -run 'Hook'

echo "OK: staging validation tests passed."
