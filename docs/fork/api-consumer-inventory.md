# Woodpecker API consumer inventory

Audit date: 2026-07-23. Fork baseline: `main` at v3.9.0 + QuintoAndar patches.

## woodpecker-go client surface

The in-repo SDK ([`woodpecker-go/woodpecker/interface.go`](../../woodpecker-go/woodpecker/interface.go)) exposes v3 endpoints only:

| Area | Endpoints / methods | v2-only? |
|------|---------------------|----------|
| Users | `/api/user`, `/api/users` | No |
| Repos | `/api/repos`, `/api/repos/lookup/{name}` | No (`/api/user/repos` deprecated alias not used in SDK) |
| Pipelines | `/api/repos/{id}/pipelines` | No (`/api/builds` removed in v3) |
| Agents | `/api/agents`, `/api/agents/{id}/tasks` | No (tasks scoped to agent, not global task API) |
| Secrets / registries / crons | standard v3 paths | No |

**Removed in upstream v3.0 (not present in fork SDK or server routes):**

- Global `/api/tasks` exposure ([#4108](https://github.com/woodpecker-ci/woodpecker/pull/4108))
- `/api/builds` aliases (pipelines only)
- `secrets:` YAML key (use `from_secret` in `environment`)

## In-repo consumers

| Consumer | Location | v2 risk |
|----------|----------|---------|
| CLI | `cli/` | Uses v3 pipeline APIs |
| OpenAPI / docs | `cmd/server/openapi/` | Documents v3 routes |
| Internal tests | `woodpecker-go/woodpecker/*_test.go` | v3 paths only |

No in-repo references to `/api/builds`, `/api/tasks` (global), or `woodpecker/v2` module path.

## External consumers (action required)

Search QuintoAndar org repos for these patterns before deploying v3.16:

```bash
# Run from org root or use internal code search
rg '/api/builds|/api/tasks[^/]|CI_BUILD_|CI_JOB_|woodpecker/v2' --type go --type py --type ts
rg 'from_secret|secrets:' .woodpecker*.yml .woodpecker/**/*.yml
```

Document any hits in your deployment runbook. Known integration from fork history:

- **External API integration** — addressed during v3.9 import ([#31](https://github.com/quintoandar/woodpecker/pull/31)); re-validate woodpecker-go consumers after v3.16 rebase.

## Verdict

**No v2-only API usage found in this repository.** External consumers must be verified separately; run [`scripts/fork/check-api-consumers.sh`](../../scripts/fork/check-api-consumers.sh) against dependent repos.
