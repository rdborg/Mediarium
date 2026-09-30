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
8. **Keep the backup file private.** A backup contains your database *and* the encryption key (`secret.key`), so whoever holds it can read every stored indexer key, Usenet password and VPN key.
9. **Update.** Security fixes only go into the latest release.
10. **Leave *Allow updates pushed through the API* off** (Settings > System > Server and backup). Switch it on only for the moment you push a file, see [below](#updating-the-program).

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

### Synology (Control Panel, Login Portal, Reverse Proxy)

Create a rule: source HTTPS, your hostname, port 443; destination HTTP, `localhost`, port 8264. Then open *Custom Header*, press *Create* and add `X-Forwarded-Proto` with the value `$scheme` (and `X-Forwarded-For` with `$proxy_add_x_forwarded_for` if your DSM version does not send it). The Synology proxy runs on the NAS itself, so its address is trusted by default. Without `X-Forwarded-Proto` everything works, but the session cookie is not marked HTTPS-only and no HSTS header is sent.

## What Mediarium does about it

- **Sign-in limits.** After five wrong passwords from one address, further sign-in attempts from it are refused until the oldest of those five is 15 minutes old (IPv6 visitors are counted per /64). Twenty-five wrong tries for one account name, counted across all addresses, does the same to that account name, so guessing from many places is slowed down too. Passwords are stored as bcrypt hashes.
- **The caller's address is only believed from a trusted proxy.** A direct visitor cannot make up an `X-Forwarded-For` header to dodge the limit. See [TRUSTED_PROXIES](#trusted_proxies).
- **Sessions.** The cookie is `HttpOnly`, `SameSite=Strict` and `Secure` whenever you reached Mediarium over HTTPS (directly, or through a trusted proxy that says so with `X-Forwarded-Proto`). A session lasts 7 days, ends when you sign out, and every session of an account ends when its password changes or is reset.
- **Cross-site requests are refused.** A web page on another site cannot make your browser change something in Mediarium: requests that change data must come from Mediarium's own address. Scripts that send an `X-API-Key` header are not affected.
- **Security headers on every page:** a Content-Security-Policy that only allows Mediarium's own scripts (and images from TMDB and the album cover service), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, a locked-down `Permissions-Policy`, and `Strict-Transport-Security` (six months) when you are on HTTPS. HSTS is never sent over plain HTTP, so visiting `http://192.168.x.x:8264` on your LAN keeps working.
- **Secrets stay with their address.** A saved password, key or token can be kept by leaving its field blank, but not when the address next to it is changed in the same edit or test: you type it again. So nobody (and no stolen API key) can point a Usenet server, indexer, media server or notification at another machine and have the saved secret sent to it. A redirect to another host drops the token headers too.
- **Basic users see less.** Pages, settings, credentials and folder paths meant for administrators are refused to basic users. A basic user who triggers a server error gets a generic message, and the details go to the log.
- **Outgoing connections avoid the cloud metadata service.** Indexer, media-server, notification, importer, download-link and torrent (tracker, web seed and magnet-link) connections are refused when they would go to `169.254.169.254`, Azure's `168.63.129.16` or another link-local or cloud-metadata address (also when written inside an IPv6 address), even through a redirect or a host name that points there. If you set `HTTP_PROXY` or `HTTPS_PROXY` for the container, Mediarium can only check the proxy's address, not where the proxy then goes. Local and LAN addresses (Prowlarr, Plex, FlareSolverr on `localhost`) still work. A basic user can only grab a release that a search showed them.
- **What is stored.** Passwords, API keys, tracker passkeys, VPN keys, media-server tokens, webhook addresses and the addresses of queued downloads are encrypted in the database with the key in `secret.key`. The database file and `secret.key` are readable only by Mediarium's own user. Sign-in passwords are stored as bcrypt hashes and API keys and sessions as SHA-256 hashes, so neither can be read back.
- **Indexers from the community list.** The list of sites is downloaded from a public project. A site's definition can only make Mediarium contact the site's own addresses when it signs in, never a stranger's, so a bad change upstream cannot collect your login or passkey. The same goes for the sign-in form on the site's own page: it can only send your login to the site's own addresses. A definition cannot make Mediarium loop for ever or build huge text, and a page or answer that is absurdly large or nested too deeply is cut short (you then see fewer results).
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

**Docker Desktop and some NAS setups make every connection look like it comes from the Docker gateway** (a private address), so Mediarium then believes forwarding headers from everyone. If you publish the port straight to the internet in such a setup, a visitor could fake their address. The fix is in the checklist: publish only the proxy, not port 8264.

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
| **New-version notice** | On | Once a day, one web request to GitHub asking for the newest release number. It only reads. | Untick *Look for new versions once a day* (Settings > System > Server and backup, in the Updates box). |
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


## Who can do what

**A basic user** can search, add titles, follow downloads, watch files in the browser and manage their own profile and API keys. They cannot read settings, credentials, folder paths (mostly), logs or other accounts, cannot start updates or backups, and see a generic message when something goes wrong on the server. They cannot make Mediarium fetch an address of their choosing: a grab only works on a release a search showed them.

**An administrator can do a great deal, by design.** They can point Mediarium at any address on your network (that is how it reaches Plex, Prowlarr or FlareSolverr on `localhost`), choose the folders it reads and deletes in, download a backup (which holds the encryption key) and, if you switch it on, replace the running program. Assume an administrator account, or an administrator's browser session, can do anything the Mediarium container can do. So:

- Give the *Admin* role to as few people as possible, and to nobody who does not need it.
- An **API key** is as strong as the account that made it, with a few exceptions that need a signed-in browser (making keys and accounts, backups, pushed updates; see [accounts.md](./accounts.md)). Keep keys out of shared scripts, dashboards you publish and chat logs, and revoke one you may have leaked.
- **Local and LAN addresses are reachable on purpose.** Only the cloud metadata service and other link-local addresses are refused. Mediarium's outgoing connections are not a boundary between your containers: if another container on the same network trusts "requests from the Docker network", an administrator can reach it through Mediarium.
- Do not put Mediarium on a network with things you would not want an administrator to reach.

## First run

Until the first account exists, anyone who can reach the sign-up page can create it, and that account is the administrator. So Mediarium only lets that happen without a code when the visitor is plainly at home:

- The visitor's address is on your home network: this machine, a private address (`192.168.x.x`, `10.x.x.x`, `172.16-31.x.x`), a Tailscale address or an IPv6 unique-local address. Behind a reverse proxy, the address the proxy passes on with `X-Forwarded-For` is the one that counts (see [TRUSTED_PROXIES](#trusted_proxies)).
- **and** the page was opened with an address or name that belongs to a home network: an IP address, `localhost`, a name without a dot (a NAS called `nas`), or one ending in `.local`, `.lan`, `.home`, `.home.arpa` or `.internal`. A public name such as `media.example.com` always needs the code, even when it points at a private address, because a web page on another site can make a visitor's browser talk to your home network under a name of its own.

Everyone else has to type the **setup code** from the log. Wrong guesses are limited the same way wrong passwords are (five per address per 15 minutes). A request that a proxy passed on without saying who the visitor is also counts as "not at home". Anyone on your home network can still create the account first, so create it before you let other people or devices in.

If you lose the code, restart the container: it prints a new one. To read it on a NAS without a terminal, open the container's log in the NAS's Docker app.

## Things to know

- **First run.** The first account created becomes the administrator. Create it right after the first start.
- **Reporting a bug with a log.** The log is cleaned of passwords, keys and tokens as it is written, but read it before you post it. It can still show your folder paths, the names of your indexers and the titles you download.
- **Forgot the password.** Reset it from the command line on the machine running Mediarium ([accounts.md](./accounts.md#a-forgotten-administrator-password)). Nothing can reset it over the web.
- **API keys** (Settings > Accounts > API keys) act as the account that made them, are stored hashed and can be revoked. Treat them like a password. A key cannot make new keys or accounts, change accounts, download or restore a backup (it holds the encryption key) or switch on pushed updates (those answer `403` and need a signed-in browser), so a leaked key does not survive being revoked.
- **Logs** never contain passwords, API keys or tokens (they are cleaned as they are written, both what `docker logs` shows and the copy under Settings > System; if you ever spot a secret in a log, report it, see [SECURITY.md](../SECURITY.md)). Failed sign-ins are logged with the account name and the address they came from, which is what tools such as fail2ban need.
- **Reporting a problem:** see [SECURITY.md](../SECURITY.md).
