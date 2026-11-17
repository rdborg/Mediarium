# Running Mediarium on the internet

Mediarium is built for your home network. To reach it from outside (a phone on mobile data, a family member's house), put it behind a reverse proxy that speaks HTTPS, or use a VPN such as WireGuard or Tailscale, which needs none of this page. Do not forward port 8264 straight from your router: Mediarium itself only speaks plain HTTP.

This page has a checklist, examples for the common proxies, and a list of what Mediarium already does for you.

## The checklist

1. **Put HTTPS in front.** Use Nginx Proxy Manager, Traefik, Caddy, Cloudflare Tunnel or the reverse proxy of your NAS. Only the proxy is reachable from the internet.
2. **Create your administrator account before you publish the address.** Until it exists, whoever opens the address first gets to create it. From your home network that just works. From anywhere else (a stranger through your proxy, or you on a server in a data centre) Mediarium asks for a one-time **setup code**, which it writes to its log every time it starts without an account: run `docker logs mediarium` and look for `setup_code`. The code is new on every start and useless once the account exists. See [first run](#first-run).
3. **Do not publish port 8264 next to the proxy.** In `docker-compose.yml` bind it to the machine itself, for example `"127.0.0.1:8264:8264"`, or put the proxy and Mediarium in the same Docker network and remove the `ports:` line. Otherwise the app can be reached without the proxy.
4. **Make the proxy forward the caller's address and scheme** (the examples below do). Mediarium uses them to count failed sign-ins per person and to mark the session cookie as HTTPS-only. Also make it **redirect plain `http://` to `https://`** (in Nginx Proxy Manager tick *Force SSL*): otherwise a password typed on an `http://` address travels in the clear.
5. **Leave `TRUSTED_PROXIES` alone if your proxy is on the same machine, in the same Docker network or on your LAN.** Set it if the proxy is somewhere else, see [below](#trusted_proxies).
6. **Use a long, unique password** for every account, and give family members the *Basic user* role, not *Admin* (see [accounts.md](./accounts.md)).
7. **Don't add WebSocket or path-rewriting settings you don't need.** Mediarium only needs plain HTTP proxying. Don't strip the `Host`, `Origin` or `Sec-Fetch-*` headers.
8. **Keep the backup file private.** A backup contains your database *and* the encryption key (`secret.key`), so whoever holds it can read every stored indexer key, Usenet password and VPN key. [Cloud backups](./INSTALL.md#cloud-backups) are locked with your passphrase before they leave the server; choose a long one.
9. **Update.** Security fixes only go into the latest release.
10. **Leave *Allow updates pushed through the API* off** (Settings > Basics > General). Switch it on only for the moment you push a file, see [below](#updating-the-program).

## Example proxy setups

Every example forwards the original `Host`, the caller's address and the scheme.

### Nginx Proxy Manager

Add a Proxy Host: scheme `http`, forward host `mediarium` (or the machine's address), port `8264`, switch on *Block Common Exploits*, and on the SSL tab request a certificate with *Force SSL* and *HTTP/2*. NPM forwards `Host`, `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Real-IP` by default. Do not add access-list rules that also try to sign in for Mediarium unless you want a second login.

### Traefik (labels)

```yaml
labels:
  - traefik.enable=true
  - traefik.http.routers.mediarium.rule=Host(`media.example.com`)
  - traefik.http.routers.mediarium.entrypoints=websecure
  - traefik.http.routers.mediarium.tls.certresolver=letsencrypt
  - traefik.http.services.mediarium.loadbalancer.server.port=8264
```

Traefik passes the original `Host` and adds `X-Forwarded-For` and `X-Forwarded-Proto`.

### Caddy

```
media.example.com {
    reverse_proxy mediarium:8264
}
```

Caddy sets `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Forwarded-Host` itself.

### Nginx

```
location / {
    proxy_pass http://127.0.0.1:8264;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 1g;   # restoring a backup uploads a zip
    proxy_buffering off;       # video playback starts sooner
}
```

Without `proxy_set_header Host $host;` nginx sends its own address as `Host`, which the cross-site check cannot match to the address in your browser (see `ALLOWED_ORIGINS`).

### Cloudflare Tunnel

Point the public hostname at `http://mediarium:8264`. `cloudflared` runs as a container in the same Docker network, so its address is private and trusted by default, and it passes the visitor's address on. If you also put Cloudflare Access in front, that is a second login on top of Mediarium's: fine, but API-key clients (scripts, other apps) then need a Cloudflare service token.

### Under a sub-path of a domain

To serve Mediarium at something like `https://home.example.com/mediarium` instead of its own host name, set `URL_BASE=/mediarium` on the container and forward that path to port 8264. The proxy examples, what changes and how to check it are in [INSTALL.md](./INSTALL.md#running-under-a-sub-path-url-base). The session cookie is then limited to that path.

### Synology (Control Panel, Login Portal, Reverse Proxy)

Step by step, with the certificate and router: [Reach it from outside your home](./synology.md#reach-it-from-outside-your-home). In short: create a rule with source HTTPS, your hostname, port 443; destination HTTP, `localhost`, port 8264. Then open *Custom Header*, press *Create* and add `X-Forwarded-Proto` with the value `$scheme` (and `X-Forwarded-For` with `$proxy_add_x_forwarded_for` if your DSM version does not send it). The Synology proxy runs on the NAS itself, so its address is trusted by default. Without `X-Forwarded-Proto` everything works, but the session cookie is not marked HTTPS-only and no HSTS header is sent.

## What Mediarium does about it

- **Sign-in limits.** After five wrong passwords from one address, further sign-in attempts from it are refused until the oldest of those five is 15 minutes old (IPv6 visitors are counted per /64). Twenty-five wrong tries for one account name, counted across all addresses, does the same to that account name, so guessing from many places is slowed down too. Wrong two-step codes count the same way, per address and per account, and signing in again with the right password does not reset that count. Wrong tries at your own password when changing it, adding a passkey, linking single sign-on or changing two-step sign-in are counted per account as well. The 25 tries per account name mean that someone who knows a name can keep its owner out for up to 15 minutes; that is the price of stopping guessing from many addresses. Each try is counted before the password is checked, so a burst of requests sent at the same moment gets the same five tries as a slow one; a right password gives its try back. Passwords are stored as bcrypt hashes.
- **The caller's address is only believed from a trusted proxy.** A direct visitor cannot make up an `X-Forwarded-For` header to dodge the limit. See [TRUSTED_PROXIES](#trusted_proxies).
- **The other ways to sign in are checked as hard.** Single sign-on, directory sign-in and passkeys are off until an administrator switches them on, and they count wrong tries against the same limits as a password (per address and per account). See [Single sign-on, directory sign-in and passkeys](#single-sign-on-directory-sign-in-and-passkeys).
- **Sessions.** The cookie is `HttpOnly`, `SameSite=Strict` and `Secure` whenever you reached Mediarium over HTTPS (directly, or through a trusted proxy that says so with `X-Forwarded-Proto`). A session lasts 7 days, ends when you sign out, can end sooner when a browser sits unused (see below), and every session of an account ends when its password changes or is reset. **Settings > People and access > Security > Signed-in devices** lists where you are signed in and has **Sign out all other devices**, for a lost phone or a shared computer (from the web page only, not with an API key). If Mediarium cannot tell whether an account has two-step sign-in on (the database is busy or damaged), it refuses the sign-in instead of letting the password alone through.
- **Sign out idle browsers.** In **Settings > People and access > Security** an administrator can choose 1 to 7 days (it is off at first). A browser session that wasn't used for that long ends, and the sign-in page says "You were signed out because of inactivity." It applies to browser sessions only: API keys, calendar feeds and the e-reader passwords keep working. The time of the last use is written at most once a minute per session. A session that ended this way stays ended even if you switch the setting off, and is removed with the other old sessions. A sign-in ends after 7 days in any case, so a longer limit would never apply and isn't offered. Setting it: `PUT /api/auth/idle-signout` with `{"days": 3}` (administrators only; `0` turns it off).
- **Cross-site requests are refused.** A web page on another site cannot make your browser change something in Mediarium: requests that change data must come from Mediarium's own address. Scripts that send an `X-API-Key` header are not affected.
- **Security headers on every page:** a Content-Security-Policy that only allows Mediarium's own scripts (images from TMDB and the album cover service, and the YouTube player for the trailer behind a title page if you switch it on), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, a locked-down `Permissions-Policy`, and `Strict-Transport-Security` (six months) when you are on HTTPS. HSTS is never sent over plain HTTP, so visiting `http://192.168.x.x:8264` on your LAN keeps working.
- **Secrets stay with their address.** A saved password, key or token can be kept by leaving its field blank, but not when the address next to it is changed in the same edit or test: you type it again. So nobody (and no stolen API key) can point a Usenet server, indexer, media server or notification at another machine and have the saved secret sent to it. Switching an address from `https://` to `http://` counts as a change too, and so does emptying an address that then falls back to a default (ntfy's public server) or switching an email server's encryption off. A redirect to another host, or from `https://` to `http://`, drops the token headers too.
- **The metrics page is closed.** `/metrics` (for Prometheus) answers only to an administrator's API key, sent as `Authorization: Bearer <key>` or `X-API-Key`, or to an administrator signed in. Everything else gets 401. A bearer header is accepted on that one address only; every other page ignores it. See [Monitoring with Prometheus](./INSTALL.md#monitoring-with-prometheus).
- **Viewers and read-only keys can't change anything.** A viewer account and a read-only API key are refused every request that changes something, in one place before any page of the app runs, for every route including the Radarr and Sonarr style ones. A test sends a changing request to every route as each of them and fails if one gets through. See [accounts.md](./accounts.md#viewers).
- **Basic users see less.** Pages, settings, credentials and folder paths meant for administrators are refused to basic users. A basic user who triggers a server error gets a generic message, and the details go to the log.
- **Outgoing connections avoid the cloud metadata service.** Indexer, media-server, notification, importer, download-link and torrent (tracker, web seed and magnet-link) connections are refused when they would go to `169.254.169.254`, Azure's `168.63.129.16` or another link-local or cloud-metadata address (also when written inside an IPv6 address), even through a redirect or a host name that points there. With the proxy in Settings switched on, the place a request goes is checked before it is handed to the proxy: an address as it is, and a host name by looking it up here first (with a `socks5h://` proxy names are left to the proxy, so they never leak; only an address written as numbers is checked). Pages FlareSolverr opens for a site are checked the same way, and an answer that ended on a refused address is thrown away. If you set `HTTP_PROXY` or `HTTPS_PROXY` for the container, Mediarium can only check the proxy's address, not where the proxy then goes. Local and LAN addresses (Prowlarr, Plex, FlareSolverr on `localhost`) still work. A basic user can only grab a release that a search showed them.
- **What is stored.** Passwords, API keys, tracker passkeys, VPN keys, media-server tokens, webhook addresses and the addresses of queued downloads are encrypted in the database with the key in `secret.key`. The database file and `secret.key` are readable only by Mediarium's own user. Sign-in passwords are stored as bcrypt hashes and API keys and sessions as SHA-256 hashes, so neither can be read back.
- **Saving settings is all or nothing.** The settings pages (and `PUT /api/settings`) check every value first and then store them together in one database transaction, keys and passwords included. If one value is refused or can't be written, none of the others is saved, so a failed save never leaves a half-changed setup behind. A blank password field next to an unchanged user name keeps the saved password as it is stored: a saved password that can't be read is never replaced by an empty one.
- **Indexers from the community list.** The list of sites is downloaded from a public project. A site that needs a login, cookie, API key or passkey can only make Mediarium contact the address you chose for the site (and other names on the same domain, such as its `api.` address), never a stranger's, so a bad change upstream cannot collect your login or passkey. Other addresses listed in the definition don't count unless you picked one of them. A public site where you have entered nothing secret may search on another public host (some use a separate address for their API), but never on your own machine or home network, and that includes a name that looks public but points there. "Nothing secret" counts any setting you fill in as text or a password, not only ones called password or key. The same goes for the sign-in form on the site's own page: it can only send your login to the site's own addresses. Links and redirects a site on the internet sends can't point Mediarium at your own machine or home network. A definition cannot make Mediarium loop for ever or build huge text, and a page or answer that is absurdly large or nested too deeply is cut short (you then see fewer results).
- **Files.** Playing a file only works inside the title's own folder; symbolic links pointing out of it are refused. Backups are checked before a restore is applied.
- **Downloads are treated as hostile.** A release is written by strangers. RAR and ZIP files are unpacked by Mediarium itself, which refuses paths that leave the download folder, links, encrypted files and archives that would unpack to more than 200 GB. `.7z` files go through the 7z tool, but only after their listing has been checked (no links, no special files, no unsafe paths, not too big), into a private folder that is inspected before anything is moved. The importer only ever copies or links plain files, never a link, and a release whose message ids or group names contain line breaks is refused before it can send anything to your Usenet provider. An article that claims a place far beyond the end of its file is treated as a bad article, so it cannot make a huge file, and a news server that never stops sending cannot use all the memory.

## TRUSTED_PROXIES

`TRUSTED_PROXIES` is a container environment variable. It lists the proxies whose `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Forwarded-Host` headers Mediarium believes. Headers from anyone else are ignored.

| Value | Meaning |
|---|---|
| `private` (default) | The machine itself (`127.0.0.1`, `::1`), private networks (`10.x`, `172.16-31.x`, `192.168.x`, `100.64-127.x` for Tailscale) and IPv6 unique-local addresses. Right for a proxy on the same host, in the same Docker network or on your LAN. |
| `none` | Trust no proxy. Use it when Mediarium is reached directly and never through a proxy. |
| addresses and ranges, comma-separated | For example `203.0.113.10` or `10.0.0.0/8, 2001:db8::/32`. Only these are trusted. Add `private` to the list to keep the default as well. |

```yaml
environment:
  - TRUSTED_PROXIES=private,203.0.113.10
```

The caller's address is taken from the *right* end of `X-Forwarded-For`, skipping trusted proxies, so entries the visitor typed themselves are never used. If you chain proxies (for example Cloudflare, then your own proxy), add the outer proxy's ranges as well; otherwise every visitor looks like the outer proxy's address and shares one sign-in limit. Cloudflare publishes its ranges at <https://www.cloudflare.com/ips/>.

**Docker Desktop, some NAS setups, rootless Podman and Docker's own port forwarder (docker-proxy, for example for IPv6 visitors to an IPv4-only container) make connections look like they come from the Docker gateway** (a private address), so Mediarium then believes forwarding headers from everyone. If you publish the port straight to the internet in such a setup, a visitor could fake their address and dodge the per-address sign-in limit (the per-account limit still holds). The fix is in the checklist: publish only the proxy, not port 8264. If you use no proxy at all, set `TRUSTED_PROXIES=none`; if you do, list the proxy container's own address instead of `private`.

## Sign in through your reverse proxy

Some proxies ask for the login themselves before anyone reaches the app: Authelia, Authentik, Cloudflare Access, oauth2-proxy and the like. They can pass the signed-in user name on in a header (often `Remote-User`). Mediarium can use it so people don't sign in twice.

It is **off** by default. A plain reverse proxy (the one on a Synology, Nginx Proxy Manager, Caddy, Traefik without an auth middleware) doesn't log anyone in, so leave it off for those.

To switch it on, as an administrator open **Settings > People and access > Ways to sign in** through your proxy, find **Sign in through your reverse proxy**, type the header name your proxy sends and your proxy's address, and press **Turn on**. When you open the page through the proxy, the address box is already filled in with the address the page came from, which is your proxy. The card then shows whether this page came from that address and what the proxy put in the header, so you can check it works before you rely on it.

How it behaves:

- The header is only believed when the connection comes straight from your proxy's address (one address, or a small range such as `172.18.0.0/24`). From anyone else it is ignored, even from other machines on your network. `private` is not accepted here: a whole network is too wide.
- The name must match an existing Mediarium account (upper and lower case don't matter). Unknown names are refused; accounts are not created by themselves. Make the accounts under Settings > People and access with the same user names your proxy uses.
- An API key or a normal Mediarium session still works and comes first. A wrong API key is refused even if the header is there.
- It can only be switched on from a signed-in browser, not with an API key. Every change is written to the log.
- Signing out of Mediarium doesn't sign you out of the proxy. Use your proxy's own sign-out.

**Important:** the proxy must remove the header from whatever the visitor sends, and set it itself (Authelia, Authentik and oauth2-proxy do). And Mediarium's port should only be reachable through the proxy. If something else could connect from the proxy's address (for example a Docker gateway address shared by everything outside the container, see the note under TRUSTED_PROXIES), it could claim to be anyone, so use the proxy container's own address rather than the gateway's when you can.

Updating from 2.1.0: sign-in through the proxy then only needed the header, and believed it from any private address. It now also needs the proxy's address, so it stays off until you open the card through your proxy and save once more.

## ALLOWED_ORIGINS

Normally not needed. The cross-site check compares the address your browser used with the `Host` header (or `X-Forwarded-Host` from a trusted proxy). Current browsers also send a `Sec-Fetch-Site` header that says where the request came from, and that works whatever the proxy does. If a proxy rewrites `Host` to its own name *and* your browser is old enough not to send that header, every save fails with "Blocked: this request came from another web site". Either make the proxy pass `Host` on (best), or list the public address:

```yaml
environment:
  - ALLOWED_ORIGINS=media.example.com
```

## Updating the program

Three things in Mediarium deal with new versions, and the last two can put a new program in place. All three are limited to administrators. The last two are checked before they run, written to the log and the Activity page, and undone with **Go back to the image's version** (or by removing the installed file). None of them needs the Docker socket or extra privileges, and the last two only work in the Docker images, on Linux.

| What | Default | What it allows | How to leave it out |
|---|---|---|---|
| **New-version notice** | On | Once a day, one web request to GitHub asking for the newest release number. It only reads. | Untick *Look for new versions once a day* (Settings > Basics > General, in the Updates box). |
| **Update now** and *Install new versions overnight* | The button is there when a release is signed; overnight is off | Downloads a release from the official GitHub repository and installs it, but only if the release's checksum list carries a valid signature from the Mediarium project's key, and the download matches that list. | Do not press the button and leave the overnight switch off. A release with no signature is never installed. |
| **Allow updates pushed through the API** | **Off** | An administrator can replace the running program by uploading a file to `POST /api/system/update`. | Leave the switch off. While it is off, the endpoint answers `403`. |

The third one is the powerful one: while it is on, whoever holds an administrator's API key can make your server run a program of their choosing. That is why it is off, and why it can only be switched on from a signed-in browser session (an API key can switch it off but never on). Switch it on when you need it and off again afterwards. Removing an installed update always works.

What is checked, every time, before a new program is installed:

- **Who:** only an administrator's session or API key. Basic users get `403`.
- **A checksum that you supply** (pushed updates): `X-Update-SHA256` is required and must match the uploaded bytes. Without it, or if it differs, the file is thrown away without being started.
- **A signature that the project supplies** (Update now): the release's `sha256sums.txt` must verify against the public key built into Mediarium (ed25519), the archive must match the signed list, and downloads may only come from `github.com` and GitHub's own file hosts (each redirect is checked; link-local and cloud-metadata addresses are refused as everywhere else).
- **Size and type:** at most 200 MB, a Linux program for the same processor, that answers `--version-check` as a Mediarium program. It is started once with an empty environment and nothing else, only to print its version.
- **Version:** newer than the running one, and never older than the one inside the Docker image (`?force=true` allows the same or an older one above that, for going back).
- **One at a time**, and the previous program is kept.

And afterwards: the entrypoint of the Docker images starts an installed program only as the app's own user (never as root), only if it still matches its recorded checksum, and puts it aside if it stops within 20 seconds of starting three times in a row. Every install is logged with the account and the checksum, never a key. The installed files are in `/config/update/`; anyone who can write to that folder can already read your database and encryption key, so they gain nothing new there.

**Updates and the risk to the project's signing key.** The private key that signs releases lives only in a protected GitHub environment (see [RELEASING.md](./RELEASING.md#protecting-the-signing-key)); the public half is built into every release. If that key ever leaked, someone could publish an update that installs on every copy with **Update now** switched on. That is why **Install new versions overnight** is off until you switch it on, and why you can leave it off and still update by hand.

**Pushing a program file is code execution.** With *Allow updates pushed through the API* on, an administrator's API key or session can make the server run any program file that comes with its own checksum. The checksum only proves the upload wasn't damaged, since the sender supplies it. The file is started once with an empty environment to read its version, then runs with the same access Mediarium has, including your database and encryption key. That is the point of the feature (it is how a test build gets onto a server), and why it can only be switched on from a signed-in browser. Leave it off unless you're about to push a file, and switch it off afterwards. **Update now** is different: it only installs files that carry the project's signature.

**Reverse proxy tip:** if you publish Mediarium, you can also refuse `POST /api/system/update` (the push endpoint, and only that exact address) at the proxy as a second lock. Nothing needs it from the internet: pushing is done from your own network.


## Two-step sign-in

If Mediarium can be reached from the internet, switch on two-step sign-in for every account that can (at least the administrators): each person under **Settings > People and access > Security > Two-step sign-in**, with any authenticator app. The password then isn't enough on its own. The secret is stored encrypted with the server key, recovery codes only as hashes, a code can't be used twice, and wrong codes count towards the same sign-in limits as wrong passwords. Details and the recovery steps: [accounts.md](./accounts.md#two-step-sign-in).

## Single sign-on, directory sign-in and passkeys

All three are off by default, and none of them takes the password away unless an administrator who has already used them says so. What each one does to keep itself safe:

**Single sign-on (OpenID Connect).**

- The sign-in uses the authorization code flow with PKCE (S256). Mediarium makes a new random `state` and `nonce` for each sign-in; each is good once, for ten minutes, and is thrown away on the first try, right or wrong.
- A short-lived cookie (`HttpOnly`, `SameSite=Lax`, only for the sign-in addresses) ties the answer to the browser that started the sign-in. Someone who tricks a visitor into opening a sign-in link they started can't end up signed in as the attacker, and a leaked answer is useless in another browser. The cookie is `Lax`, not `Strict`, because the browser comes back from the provider's site.
- The ID token is checked with the maintained go-oidc library: signature against the provider's own published keys (algorithms from the provider's list, never `none` or a shared-secret one), issuer, audience, expiry and not-before. Mediarium adds the nonce, the authorized party and the subject. Claims the token doesn't carry are asked for at the provider's user info address, and only believed if they are about the same person.
- The address the provider sends people back to is made from the address you set under **Settings > Basics > General > Address of Mediarium**, never from what a request says (a forged `Host` header changes nothing).
- The only place the browser goes after signing in is a path on Mediarium's own site; `//other.example`, `https://...` and `\` forms fall back to the start page. A problem is shown as one of a few fixed sentences, never as text from the provider.
- The client secret is stored encrypted, is never sent back to the browser, and doesn't follow a change of the provider's address (retype it).
- Accounts are tied to the provider's own id for the person (issuer and subject), never to a name or an email address. A new account never takes over an existing one, even with the same user name. Linking by email needs the provider to say the address is verified, only uses an address an administrator set on the account (never one the person typed on their own profile), never links an admin, and is off unless you switch it on.

**Directory sign-in (LDAP, Active Directory).**

- The connection is encrypted (`ldaps://`, or StartTLS) and the certificate is checked, against the system's authorities plus the one you paste. Sending passwords without encryption, or skipping the check, are separate, clearly named choices. If StartTLS fails nothing is sent in the clear.
- What is typed goes into the search escaped (RFC 4515), so `*`, `(`, `)` and `\` can't change it, and names are limited in length and in characters. An empty password is refused before the directory is asked, since many servers treat an empty bind as a good anonymous one. A search that finds no entry, or more than one, signs nobody in.
- A wrong password and an unknown user get the same answer. The bind account's password is stored encrypted, never sent back, and doesn't follow a change of the server address. Every network step has a time limit, and the connection goes through the same guard that keeps Mediarium's outgoing connections away from the cloud metadata service.
- The password of a directory account is checked by the directory and never stored here; the account has no password hash at all, and no setting or command can give it one.

**Passkeys.**

- Built on the maintained go-webauthn library: it checks the challenge, the origin, the relying party id, user presence and verification, the signature and the backup flags. Mediarium adds a challenge store (each challenge works once, for five minutes, and only for the kind of step it was made for) and refuses a passkey whose counter doesn't go up (a sign it was copied).
- The relying party id and the origin come from the public address you saved, which has to be `https://` with a host name (or `http://localhost`). A passkey made for `mediarium.example.com` never works on another address.
- User verification is required, so a passkey is something you have and something you are or know. Adding a passkey needs your password, so a stolen session can't leave a way back in. Only accounts with a password here can have passkeys.

**Limits.** Mediarium asks the provider or directory when someone signs in, not afterwards. An account removed there can't sign in again, but a session it already has lasts until it ends (up to 7 days). **Edit > Turn this account off** ends it at once. Your provider's own security (its sign-in page, its second step, its account recovery) is as strong as this gets: whoever can sign in there as an admin can sign in here.

**Getting back in.** `MEDIARIUM_DISABLE_SSO=1` on the container switches single sign-on and directory sign-in off and the password form on, whatever is saved. Password sign-in can only be turned off by an administrator who is signed in through single sign-on, the directory or a passkey, and turns itself back on when no other way works. See [accounts.md](./accounts.md#turning-password-sign-in-off).

## Who can do what

**A basic user** can search, add titles, follow downloads, watch files in the browser and manage their own profile and API keys. They cannot read settings, credentials, folder paths (mostly), logs or other accounts, cannot start updates or backups, and see a generic message when something goes wrong on the server. They cannot make Mediarium fetch an address of their choosing: a grab only works on a release a search showed them.

**A viewer** can look and play, and change nothing: not the library, not downloads, not their own API keys (they can't have any), and they can't see settings, other accounts or the secret calendar address. A **read-only API key** is the same for scripts: `GET` only.

**An administrator can do a great deal, by design.** They can point Mediarium at any address on your network (that is how it reaches Plex, Prowlarr or FlareSolverr on `localhost`), choose the folders it reads and deletes in, download a backup (which holds the encryption key) and, if you switch it on, replace the running program. Assume an administrator account, or an administrator's browser session, can do anything the Mediarium container can do. So:

- Give the *Admin* role to as few people as possible, and to nobody who does not need it.
- An **API key** is as strong as the account that made it, with a few exceptions that need a signed-in browser (making keys and accounts, backups, pushed updates; see [accounts.md](./accounts.md)). Keep keys out of shared scripts, dashboards you publish and chat logs, and revoke one you may have leaked.
- **Local and LAN addresses are reachable on purpose.** Only the cloud metadata service and other link-local addresses are refused. Mediarium's outgoing connections are not a boundary between your containers: if another container on the same network trusts "requests from the Docker network", an administrator can reach it through Mediarium.
- Do not put Mediarium on a network with things you would not want an administrator to reach.

## First run

Until the first account exists, anyone who can reach the sign-up page can create it, and that account is the administrator. So Mediarium only lets that happen without a code when the visitor is plainly at home:

- The visitor's address is on your home network: this machine, a private address (`192.168.x.x`, `10.x.x.x`, `172.16-31.x.x`), a Tailscale address or an IPv6 unique-local address. Behind a reverse proxy, the address the proxy passes on with `X-Forwarded-For` is the one that counts (see [TRUSTED_PROXIES](#trusted_proxies)).
- **and** the page was opened with an address or name that belongs to a home network: a home address like the ones above (not a public IP address), `localhost`, a name without a dot (a NAS called `nas`), or one ending in `.local`, `.lan`, `.home`, `.home.arpa` or `.internal`. A public name such as `media.example.com` always needs the code, even when it points at a private address, because a web page on another site can make a visitor's browser talk to your home network under a name of its own.

Opening the page by your server's public IP address always needs the code. This matters on a plain Linux Docker host too: Docker's own port forwarder (docker-proxy) hands a visitor who reaches an IPv4-only container over IPv6, or who comes from this very computer, to Mediarium from the private Docker gateway address, and rootless Podman does the same over IPv4. Such a visitor looks the same whether they are at home or on the internet, and both the `Host` and `X-Forwarded-For` headers are whatever they chose to send. So a request that arrives from the container's default gateway address **always needs the setup code**, whatever its headers say.

In practice: on a Docker install, the first account made from the same computer (`http://localhost:8264`) asks for the setup code. Read it with `docker logs mediarium` and look for `setup_code`; it is printed every time Mediarium starts with no account. A phone or laptop on your home network that reaches the server directly is still let in without it.

Everyone else has to type the **setup code** from the log. Wrong guesses are limited the same way wrong passwords are (five per address per 15 minutes). A request that a proxy passed on without saying who the visitor is also counts as "not at home". The same goes for a proxy that runs on the Docker host and reaches the container through the gateway address. Anyone on your home network can still create the account first, so create it before you let other people or devices in.

If you lose the code, restart the container: it prints a new one. To read it on a NAS without a terminal, open the container's log in the NAS's Docker app.

## Things to know

- **First run.** The first account created becomes the administrator. Create it right after the first start.
- **Reporting a bug with a log.** The log is cleaned of passwords, keys and tokens as it is written, but read it before you post it. It can still show your folder paths, the names of your indexers and the titles you download.
- **Forgot the password.** Reset it from the command line on the machine running Mediarium ([accounts.md](./accounts.md#a-forgotten-administrator-password)). Nothing can reset it over the web.
- **API keys** (Settings > People and access > Security > API keys) act as the account that made them, are stored hashed and can be revoked. Treat them like a password. A key cannot make new keys or accounts, change accounts, download or restore a backup (it holds the encryption key) or switch on pushed updates (those answer `403` and need a signed-in browser), so a leaked key does not survive being revoked.
- **Logs** never contain passwords, API keys or tokens (they are cleaned as they are written, both what `docker logs` shows and the copy under Settings > System; if you ever spot a secret in a log, report it, see [SECURITY.md](../.github/SECURITY.md)). Failed sign-ins are logged with the account name and the address they came from, which is what tools such as fail2ban need.
- **Reporting a problem:** see [SECURITY.md](../.github/SECURITY.md).
