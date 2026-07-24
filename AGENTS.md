# AGENTS.md

Woodpecker CI is a CI/CD engine. This repo contains three Go binaries and a web UI:

- `cmd/server` — the server (HTTP UI/API on `:8000`, gRPC for agents on `:9000`). Requires CGO (SQLite) — a C compiler (`gcc`) must be present.
- `cmd/agent` — the agent that runs pipeline steps (backends: `docker`, `local`, `kubernetes`).
- `cmd/cli` — the `woodpecker-cli` command (includes `exec` to run a pipeline locally).
- `web/` — the Vue 3 + Vite UI (embedded into the server binary at `web/dist`).
- `docs/` — Docusaurus documentation site.

Standard build/lint/test commands live in the `Makefile` (`make help`) and `docs/docs/92-development/01-getting-started.md`. Use those as the source of truth.

## Cursor Cloud specific instructions

Dependencies (Go modules + `web/` pnpm packages) are already installed by the startup update script. `gcc`, Go, Node, and pnpm are preinstalled.

### Running the full app in dev mode (server + agent + UI)

The three processes all auto-load a `.env` file from the repo root (via `godotenv/autoload`). `.env` is git-ignored, so create it if missing. A working minimal dev `.env`:

```ini
WOODPECKER_OPEN=true
WOODPECKER_ADMIN=woodpecker
WOODPECKER_HOST=http://localhost:8000
WOODPECKER_AGENT_SECRET=1234567890abcdefghij
WOODPECKER_GITEA=true
WOODPECKER_GITEA_URL=http://localhost:3000
WOODPECKER_GITEA_CLIENT=dummyclientid
WOODPECKER_GITEA_SECRET=dummyclientsecret
WOODPECKER_DEV_WWW_PROXY=http://localhost:8010
WOODPECKER_HEALTHCHECK=false
```

Then run each in its own terminal (long-running; use tmux):

- Server: `go run ./cmd/server` (or `./dist/woodpecker-server` after `make build-server`).
- Agent: `WOODPECKER_SERVER=localhost:9000 WOODPECKER_AGENT_SECRET=1234567890abcdefghij WOODPECKER_BACKEND=local WOODPECKER_HEALTHCHECK=false go run ./cmd/agent`.
- UI dev server: `cd web && VITE_DEV_SERVER_HOST=0.0.0.0 pnpm start` (serves on `:8010`).

Non-obvious caveats:

- The server **proxies the UI to the vite dev server** when `WOODPECKER_DEV_WWW_PROXY=http://localhost:8010` is set. Access the app at `http://localhost:8000` (NOT 8010). If the vite dev server is not running, `http://localhost:8000/` returns `502`.
- Inside a container, the vite dev server **must** be started with `VITE_DEV_SERVER_HOST=0.0.0.0` or the proxy can't reach it.
- The server binary **embeds `web/dist`** (`//go:embed dist/*`). A server build fails if `web/dist` is empty/missing. Run `pnpm -C web build` first, or (UI not needed) stub it with `mkdir -p web/dist && touch web/dist/index.html`. `make build-server` builds the UI for you.
- **Login requires a real forge OAuth app** (GitHub, Gitea, Forgejo, GitLab, …). With the dummy forge values above the UI renders the login page but you can't log past it. For a full logged-in demo, run a Gitea forge (see `docker-compose.gitpod.yaml` / `.gitpod.yml`) or supply real GitHub OAuth credentials. Docker is NOT preinstalled.

### Running a pipeline without a forge (self-contained)

`woodpecker-cli exec` runs a pipeline locally with no server/forge, exercising the core engine:

```bash
woodpecker-cli exec --backend-engine local path/to/.woodpecker.yaml
```

The `local` backend runs step commands directly on the host (no Docker needed). The `docker` backend requires a Docker daemon, which is not preinstalled here.

### Lint / test

- Go lint: `make lint` self-installs `golangci-lint` into `$(go env GOPATH)/bin` on first run (needs network); ensure that dir is on `PATH`. `make lint-ui` for the UI.
- Go tests: `make test-agent test-server test-cli test-lib` and `make test-server-datastore` (SQLite-backed, no external DB needed).
- UI: `make test-ui` runs eslint + prettier check + `vue-tsc` typecheck + vitest.
