# Security policy

## Supported versions

Only the latest release of Mediarium gets security fixes. Please update before
reporting, in case the problem is already fixed.

## Reporting a problem

Please **do not open a public issue** for a security problem.

Report it privately instead, using GitHub's
[private vulnerability reporting](https://github.com/rdborg/Mediarium/security/advisories/new)
("Report a vulnerability" on the Security tab).

Include what you can of:

- the Mediarium version (Settings → About) and how you run it (Docker, binary)
- what an attacker can do, and what they need first (for example, a member
  account, or access to the local network)
- steps to reproduce, or a proof of concept

You should get a first reply within 7 days. Once a fix is released, the
advisory is published and you are credited, unless you would rather not be.

## Scope

In scope: the Mediarium server, its web interface, its API, the Docker image
and the release binaries.

Out of scope: problems in the sites, indexers, Usenet providers or VPN
services you connect Mediarium to, and anything that needs an admin account
to already be compromised.

## How Mediarium protects you

- Passwords are stored as bcrypt hashes; API keys, passwords and VPN keys are
  never written to the logs.
- The container runs without extra privileges (no `NET_ADMIN`, no
  `/dev/net/tun`); the built-in VPN runs inside the app.
- Dependencies are watched by Dependabot, and every change is checked with
  `govulncheck` and `npm audit`.
