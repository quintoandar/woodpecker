# Fork justification (code-derived)

## Why the fork exists

Historical context: the fork was created at **v2.7.0** (July 2024) before upstream **v3.0** (January 2025), which removed several APIs and changed defaults. Commit [#31](https://github.com/quintoandar/woodpecker/pull/31) bulk-imported **v3.9.0** in September 2025.

The fork is **not** maintaining a large removed upstream feature. It carries **operational patches** for GitHub scale and agent DB write reduction.

## Patches still unique vs upstream v3.16

| Patch | Purpose | Upstream status |
|-------|---------|-----------------|
| GraphQL config `Dir()` | Avoid GitHub REST Contents N+1 when loading `.woodpecker/` trees | **Not in upstream** — primary fork value |
| `WOODPECKER_FORGE_TIMEOUT=15s` default | Safer forge fetch with async webhook ack | Upstream default still 5s — keep as config or fork default |
| Skip `LastWork` update on `Log()` RPC | Reduce DB writes under heavy logging | Upstream throttles to 1/min but still updates on Log — optional fork tweak |
| `go-github-ratelimit` | Secondary rate limit handling | **Lost in v3.9 import** — restore on rebase |
| CI image internalization ([#36](https://github.com/quintoandar/woodpecker/pull/36)) | QuintoAndar CI only | Keep in `.woodpecker/` |

## Patches now redundant (drop on rebase)

| Fork patch | Upstream equivalent |
|------------|---------------------|
| [#37](https://github.com/quintoandar/woodpecker/pull/37) `CI_COMMIT_PULL_REQUEST_DRAFT` | v3.16 [#6778](https://github.com/woodpecker-ci/woodpecker/pull/6778) |
| [#38](https://github.com/quintoandar/woodpecker/pull/38) async webhook 202 ack | v3.16 [#6781](https://github.com/woodpecker-ci/woodpecker/pull/6781) |

## Agent RPC: upstream already absorbed #21

Upstream v3.16 [`server/rpc/rpc.go`](../../server/rpc/rpc.go) includes `updateAgentLastWork` with 1-minute throttling (from upstream [#4031](https://github.com/woodpecker-ci/woodpecker/pull/4031)). Fork [#19](https://github.com/quintoandar/woodpecker/pull/19) additionally skips updates on `Log()` entirely — only port if DB pressure from logs is still observed in production.

## Original "removed feature" hypothesis

Likely candidates from v3.0 breaking changes (now absorbed via v3.9 import):

1. Task data API exposure — removed; consumers must use agent-scoped `/api/agents/{id}/tasks`
2. woodpecker-go pagination / model changes — fixed in [#31](https://github.com/quintoandar/woodpecker/pull/31)
3. Default PR approval from public repos — configuration, not code fork

**Team confirmation:** validate with whoever owns the original fork decision that no external system still depends on v2 global task API or `/api/builds`.
