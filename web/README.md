# Web UI

React + Vite (TypeScript) frontend for the single search bar / library / calendar / settings / onboarding wizard of the Mediarium app.

The compiled Go binary embeds this app's production build (`web/dist/`, via `go:embed` in `web/embed.go`) so the whole app ships as one binary/one port. `web/dist/index.html` in the repo is a **placeholder** — it exists only so `go build ./...` succeeds on a fresh clone without requiring Node at all. Build the real thing before you actually want to use the UI:

```bash
cd web
npm install
npm run build
```

Then rebuild the Go binary (`go build ./...` from the repo root) to pick up the new embedded files — `go:embed` bakes them in at compile time, so a running server or a stale binary won't see a frontend rebuild until it's recompiled.

`web/dist/` (other than the tracked placeholder `index.html`) is gitignored — it's a build artifact, not source. Don't commit a real `npm run build` output over the placeholder; the Dockerfile's multi-stage build regenerates it for real images.

For local frontend-only iteration against an already-running backend:

```bash
npm run dev
```

Vite's dev server proxies `/api` to `http://localhost:8080` (see `vite.config.ts`) — run the Go server separately (`go run ./cmd/app`) alongside it.
