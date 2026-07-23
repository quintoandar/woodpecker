#!/usr/bin/env bash
# ponytail: runs fork-critical unit tests; fails fast if GraphQL or RPC patches break.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

echo "==> GraphQL config dir tests"
go test ./server/forge/github/ -run 'TestDirGraphQL|TestGraphQLEndpoint' -count=1

echo "==> GitHub forge package"
go test ./server/forge/github/ -count=1

echo "==> RPC Log (no LastWork on log lines)"
go test ./server/rpc/ -run 'Log' -count=1 2>/dev/null || go test ./server/rpc/ -count=1

echo "==> Hook handler (upstream async ack; no fork duplicate)"
go test ./server/api/ -run 'Hook' -count=1

echo "OK: staging validation tests passed."
