# Security policy

## Supported versions

Only the latest release of Mediarium gets security fixes. Update before
reporting, in case it is already fixed.

## Reporting a problem

**Do not open a public issue** for a security problem.

Report it privately instead, using GitHub's
[private vulnerability reporting](https://github.com/rdborg/Mediarium/security/advisories/new)
("Report a vulnerability" on the Security tab).

Include as much of this as you can:

- the Mediarium version (Settings > System > About and credits) and how you run it
  (Docker Compose, a NAS's Docker app, built from source)
- what an attacker can do, and what they need first (for example, a member
  account, or access to the local network)
- steps to reproduce, or a proof of concept

You'll get a first reply within 7 days. Once a fix is out, the advisory is
published and you're credited, unless you'd rather not be.

## Scope

In scope: the Mediarium server, its web interface, its API, the Docker image
(`ghcr.io/rdborg/mediarium`) and the release binaries.

Mediarium serves plain HTTP and is meant for your home network. Exposing
port 8264 directly to the internet is not supported: use a VPN or a reverse
proxy with HTTPS
([docs/security.md](../docs/security.md) has the checklist).

Out of scope: problems in the sites, indexers, Usenet providers or VPN
services you connect Mediarium to, and anything that needs an admin account
to already be compromised. An administrator is powerful by design
(see "Who can do what" in [docs/security.md](../docs/security.md)), so "an
administrator can do X" is not a vulnerability by itself. "A basic user, a
stranger, an API key, a release or a web page can do X" is.

## How Mediarium protects you

- Passwords are stored as bcrypt hashes; API keys, passwords and VPN keys are
  never written to the logs (which are cleaned as they are written) and are
  encrypted in the database with a key kept in a separate file. Failed sign-ins
  are rate limited per address and per account, using the real caller address
  behind a reverse proxy (only from proxies you trust).
- On a brand-new install, creating the first account from outside the home
  network needs a one-time code from the log.
- An API key cannot make accounts or keys, download or restore a backup, or
  switch on pushed updates.
- Session cookies are HttpOnly and SameSite=Strict (and Secure on HTTPS);
  requests that change data must come from Mediarium's own address.
- Every page carries a Content-Security-Policy and the usual security headers.
- Outgoing connections (indexers, media servers, notifications,
  downloads, torrent trackers and web seeds) never go to cloud metadata or
  link-local addresses. Local and LAN addresses are reachable on purpose.
- Downloaded archives, file names and subtitles are treated as hostile: no
  links, no paths that leave the download folder, size limits.
- Program updates from GitHub install only if the release checksum list is
  signed with the project's key.
- The container runs without extra privileges (no `NET_ADMIN`, no
  `/dev/net/tun`); the built-in VPN runs inside the app.
- Dependencies are watched by Dependabot, and every change is checked with
  `govulncheck` and `npm audit`.
