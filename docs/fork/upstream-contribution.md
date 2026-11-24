# Upstreaming: GitHub GraphQL config directory fetch

## Problem

`server/forge/github.Dir()` uses REST `GetContents` then fans out one `File()` call per entry (N+1). Large `.woodpecker/` trees hit GitHub rate limits and webhook timeouts.

## Proposed solution

Add [`server/forge/github/graphql.go`](../../server/forge/github/graphql.go) to upstream:

- Single GraphQL `repository.object(expression: "{sha}:{path}")` query for one directory level
- Skip non-blob and binary entries; REST fallback only for truncated blobs
- Classify `NOT_FOUND` via structured GraphQL error type (no English substring matching)
- GitHub Enterprise: `{url}/api/graphql`; GitHub.com: `https://api.github.com/graphql`

## PR checklist for woodpecker-ci/woodpecker

1. Copy `graphql.go` + `graphql_test.go` (update `New(id, opts)` signature)
2. Change `Dir()` to call `dirGraphQL` with REST fallback on GraphQL failure (optional conservative rollout)
3. Wire authenticated HTTP client through existing `newClientToken` oauth transport
4. Add changelog entry under Enhancement
5. Reference GitHub webhook 10s deadline in docs

## Fork maintenance impact

If accepted upstream, QuintoAndar can drop the largest fork-only patch and track vanilla releases with only:

- Forge timeout default (or deployment config)
- Optional `Log()` LastWork skip
- CI image pins

## Draft PR title

```
feat(github): fetch config directory via GraphQL to avoid REST N+1
```
