# Browser tests

These tests drive Mediarium in a real browser with [Playwright](https://playwright.dev). They need a running Mediarium with an **empty config folder**, because the first test goes through the setup wizard and creates the administrator.

```bash
# 1. Start a throwaway copy (any free port, a new empty folder for /config)
docker run -d --name mediarium-e2e -p 8301:8264 -v "$PWD/e2e-config:/config" ghcr.io/rdborg/mediarium:latest

# 2. Run the tests against it
cd web
MEDIARIUM_URL=http://localhost:8301 npx playwright test
```

- `npx playwright install chromium` downloads the browser once. To use Edge or Chrome that's already installed, set `PW_CHANNEL=msedge` or `PW_CHANNEL=chrome` instead.
- The setup test saves the session and the generated password in `e2e/.auth/` (ignored by git), and the other tests reuse them.
- To run them again, start over with a new, empty config folder.
- The same tests run at three sizes: `--project app` is the desktop run, `--project app-tablet` (820x1180) and `--project app-phone` (390x844, touch) reuse the administrator the first run created, so add `--no-deps` to them after the first run.
- On a failure, `npx playwright show-report` shows a screenshot and a trace of each failed step.

What they cover: the setup wizard from a fresh install, signing in (a wrong password is refused), every main page opening without an error, demo mode, the dark/light switch and changing the language.

`urlbase.check.mjs` is a separate script (not part of the run above) that checks Mediarium under a URL base such as `/mediarium`, or at the root. Start a container with an empty config folder and `URL_BASE=/mediarium`, then run `URL_BASE=/mediarium MEDIARIUM_URL=http://localhost:8315 node e2e/urlbase.check.mjs` from the `web` folder. It signs in, opens the main pages, and fails if any request leaves the prefix.

`../tools/docshots.mjs` is not a test: it retakes the screenshots in `docs/images` (dark, 1440 px, no version bubble, the demo account called Mediarium). Point it at a throwaway copy with made-up data (`MEDIARIUM_URL`, `SHOT_USER`, `SHOT_PASSWORD`, optionally `PW_CHANNEL=chrome`), give it the names of the shots to retake, and let `SHOT_OUT` send them to a scratch folder first so you can look at them before they replace the ones in the docs.

CI runs them on every push (the `browser` job in `.github/workflows/ci.yml`) against a freshly built server.
