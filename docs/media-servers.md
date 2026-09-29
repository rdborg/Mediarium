# Media servers (Plex, Jellyfin, Emby)

Mediarium downloads and organises your movies and shows; you watch them in Plex, Jellyfin or Emby. Connecting your media server lets the two work together:

- **New downloads show up straight away.** After each import, Mediarium asks the media server to scan the folder the new file went into, so you don't wait for its next scheduled scan.
- **Refresh now.** A button that asks the server to scan all of its libraries.
- **Watch in Plex / Jellyfin / Emby.** A title's page links straight to the same title on your media server (found by its TMDB id).
- **Open my media server.** A link to your server's own web app.

Mediarium does not play or convert video itself beyond the simple in-browser player on a title's Files card; your media server stays the place to watch.

## Find my media servers

**Find my media servers** looks for Plex, Jellyfin and Emby on your network, so you don't have to know addresses and ports. It takes about ten seconds and does two things at once:

- It **asks the network**: Jellyfin and Emby answer a broadcast on UDP port 7359, and Plex answers its "GDM" discovery on UDP port 32414.
- It **checks the usual ports** (32400 for Plex, 8096 for Jellyfin and Emby) on every address of a few networks, and asks whatever answers who it is. This needs no sign-in.

The networks checked are the ones Mediarium itself is on, plus the most common home networks (`192.168.0.x`, `192.168.1.x`, `10.0.0.x`, `10.0.1.x`, `172.16.0.x`). You can type your own instead, for example `192.168.50.0/24` or a single address like `192.168.50.10`: up to 8 networks, each at most 256 addresses (`/24`). Only private networks are ever checked (`10.x.x.x`, `172.16.x.x` to `172.31.x.x`, `192.168.x.x`, and `100.64.x.x` to `100.127.x.x`, which Tailscale uses); anything else is refused, so the search can never be pointed at the internet.

Each server found shows its type, name, address and version, and whether it is **already added**. Choose one to sign in to it (below); the address is filled in for you.

### Why the broadcast may find nothing in Docker

Broadcasts only reach devices on the same network. With Docker's default **bridge** network, Mediarium sits on a small private network inside your computer, and broadcasts from it don't reach your home network. The port check still works, as long as you search the right network: if your home network is `192.168.50.x`, type `192.168.50.0/24` and search again. The Docker network itself (usually `172.17.0.x`) is always included, which finds a media server running directly on the same computer (at `172.17.0.1`).

With `network_mode: host`, or a macvlan network, broadcasts work as normal. If nothing is found at all, add the server by its address as described in [Adding a server](#adding-a-server).

## Sign in with Plex

The easiest way to add Plex, and the one other Plex apps use:

1. Choose **Sign in with Plex**. Mediarium shows a code and opens `app.plex.tv` in a new tab.
2. Sign in to your Plex account there (if you aren't already) and approve **Mediarium**.
3. Back in Mediarium, your Plex servers are listed: the ones you own, and ones shared with you. Choose one and Mediarium adds it.

Mediarium tries the server's local addresses first and uses the first one that answers; if the address you picked doesn't answer, it tries the others. It saves that server's own access token (encrypted), not your Plex account's. Your account token is only kept in memory while you choose, is never saved, and is forgotten after 30 minutes. The sign-in itself expires after 30 minutes too; just start again.

Mediarium needs to reach `plex.tv` for this. Adding a server shared with you works, but Plex only lets the owner's token rescan libraries, so **Refresh after import** won't work on a shared server.

## Jellyfin Quick Connect

Quick Connect signs Mediarium in to Jellyfin without typing a password into it:

1. Enter the Jellyfin address (or pick it from **Find my media servers**) and choose **Quick Connect**. Mediarium shows a six-digit code.
2. In a Jellyfin app or web page where you are signed in as an administrator, open your user menu, then **Quick Connect**, and enter the code.
3. Mediarium notices within a few seconds and adds the server.

The code is valid for 10 minutes. Quick Connect has to be switched on in Jellyfin: **Dashboard, General, Quick Connect**. If it is off, Mediarium says so; switch it on, or use a username and password instead. Quick Connect is a Jellyfin feature; Emby doesn't have it.

## Username and password (Jellyfin and Emby)

Enter the server's address, your Jellyfin or Emby username and password, and choose **Sign in**. Mediarium signs in once, gets an access token from the server and saves that (encrypted). The password is sent only to your media server, and is never saved or written to the log.

Use an **administrator** account: Mediarium needs one to ask the server to rescan its libraries, and says so if the account isn't one. "Wrong username or password" means just that; "Could not reach ..." means the address or port is wrong or the server is off.

Signing in again to a server that is already added (by any of these methods) updates its address and token instead of adding it twice, which is also how to fix a token that stopped working.

## Adding a server

You can always add a server by hand, with its address and a token or API key. Go to **Settings, Media servers** and choose **Add**:

| Field | What to enter |
|---|---|
| Type | Plex, Jellyfin or Emby. |
| Address | How Mediarium reaches the server, for example `http://192.168.1.10:32400` (Plex) or `http://192.168.1.10:8096` (Jellyfin and Emby). If both run in Docker on the same network, the container name works too: `http://plex:32400`. |
| Token / API key | See below for where to find it. It is stored encrypted and never shown again; leave it blank when editing to keep the saved one. |
| Public address (optional) | The address *you* open in your browser or app, if different from the one above (for example `https://jellyfin.example.com`). "Watch in" links use it. When it is blank, the address above is used, and Plex links go through `app.plex.tv`, which works anywhere you are signed in to Plex. |
| Refresh after import | On by default. Turn it off if you'd rather the server only scans on its own schedule. |
| Path mapping (optional) | Only needed when the server sees your library under a different folder than Mediarium does. See [Path mapping](#path-mapping). |

Press **Test** before saving. The test says in plain words what is wrong: the address can't be reached, the token was refused, or the address answered but isn't a server of the chosen type. A good test lists the server's libraries and the folders they use, which helps with path mapping.

### Finding your Plex token

1. Open Plex in a web browser and sign in as the server owner.
2. Open any movie or episode, choose the **...** menu, then **Get Info**, then **View XML**.
3. A new tab opens. At the very end of its address is `X-Plex-Token=` followed by a string of letters and numbers. That string is your token.

Treat the token like a password: it gives full access to your Plex server.

### Finding a Jellyfin API key

1. In Jellyfin, open **Dashboard** (Administration), then **API Keys**.
2. Choose **+**, name it `Mediarium` and save.
3. Copy the key that appears in the list.

### Finding an Emby API key

1. In Emby, open **Settings** (Manage Emby Server), then **Advanced**, then **API Keys**.
2. Choose **New API Key**, name it `Mediarium` and save.
3. Copy the key.

## Path mapping

Mediarium tells the media server *which folder* changed, using the folder's name as Mediarium sees it. If your media server sees the same files under a different name, it won't recognise the folder. Path mapping translates the name.

Example: in Docker, Mediarium has your movies at `/movies`, but Plex has the same drive at `/data/movies`. Add one mapping:

| Mediarium folder | Media server folder |
|---|---|
| `/movies` | `/data/movies` |

Now a new film in `/movies/Heat (1995)` is reported to Plex as `/data/movies/Heat (1995)`. Add one row per library folder (for example another for `/tv`). A Windows media server works too: map `/movies` to `D:\Media\Movies`.

When nothing needs translating (both see `/movies`), leave path mapping empty.

If Plex can't match the folder to any library, Mediarium scans all of Plex's movie (or TV) libraries instead, so the new file still appears, just more slowly; the log says so and suggests checking the mapping. Jellyfin and Emby are simply told the translated folder.

## What happens after an import

- Imports are collected for about 15 seconds first, so a season pack, or several downloads finishing together, cause one scan per folder rather than one per file.
- **Plex** scans just that folder in the library that contains it (a partial scan). **Jellyfin and Emby** are told which folders changed and scan those; an old version that doesn't support this gets a full library scan instead.
- It runs in the background. A media server that is down never delays or fails an import; the outcome is written to the log.
- If a server's last test or refresh failed, the dashboard shows it ("Plex (Plex) isn't working") with the reason, until a test or refresh works again.
- Servers that are switched off, or have **Refresh after import** off, are left alone.

## Watch in and Open my media server

On a movie or show page, Mediarium asks each enabled server whether it has the title (by TMDB id) and shows a **Watch in** link for each one that does. Answers are remembered for a few minutes (and forgotten after an import or a refresh), so pages stay quick. Every account, family members included, sees these links; only administrators can see or change the servers themselves.

Matching needs the TMDB id on the media server's side:

- **Plex:** the current Plex Movie and Plex TV Series agents record TMDB ids, and so does the older "The Movie Database" agent. Titles matched only by the old IMDb or TheTVDB agents can't be found; refreshing their metadata with a current agent fixes that.
- **Jellyfin and Emby:** the TMDB metadata provider is on by default. A title without a TMDB id gets no link.

A server that can't be reached is simply left out of the links.

## Troubleshooting

| Message | What to check |
|---|---|
| Could not reach ... | The address and port, that the server is running, and that Mediarium's container can reach it (inside Docker, `localhost` means the Mediarium container itself; use the host's IP or the container name). |
| ... refused the token / API key | That the whole token or key was copied, and it hasn't been removed or regenerated. |
| That address answered, but it does not look like a ... server | The port (Plex is usually 32400, Jellyfin and Emby 8096), and that the right type is chosen. |
| This is a Jellyfin server / This looks like an Emby server | Change the type to the one the message names. |
| Secure connection (certificate) problem | Use the plain `http://` address on your local network for the Address field; keep the `https://` one as the Public address. |
| Find my media servers: nothing answered | See [Why the broadcast may find nothing in Docker](#why-the-broadcast-may-find-nothing-in-docker): type your home network and search again. |
| ... is not a private network / is too large to scan | Only private networks of at most 256 addresses can be searched, for example `192.168.1.0/24`. |
| Could not connect to "..." at any of its addresses (Sign in with Plex) | Mediarium can't reach the Plex server on any address plex.tv knows. Add it by its address instead. |
| Quick Connect is turned off | Switch it on in Jellyfin under **Dashboard, General, Quick Connect**, or sign in with a username and password. |
| ... is not an administrator on this server | Sign in with an administrator account. |

## For developers

Endpoints (administrators, except the links): `GET/POST /api/media-servers`, `PUT/DELETE /api/media-servers/{id}`, `POST /api/media-servers/test`, `POST /api/media-servers/{id}/test`, `POST /api/media-servers/{id}/refresh`, and `GET /api/media-servers/links?tmdbId=&kind=movie|tv` for every account. Details are in [reference/api.md](./reference/api.md). The code is in `internal/mediaservers`.

Finding and signing in (administrators only; no token is ever included in an answer):

| Endpoint | Body | Answer |
|---|---|---|
| `POST /api/media-servers/discover` | `{subnets?: ["192.168.1.0/24"]}` (optional) | `{found: [{kind, name, address, version?, id?, alreadyAdded, via: "broadcast"\|"scan"}], scanned: ["192.168.1.0/24"], note?}`; 400 for a public, too large or unreadable network, or more than 8 |
| `POST /api/media-servers/plex/pin` | none | `{pinId, code, authUrl, expiresIn}` |
| `GET /api/media-servers/plex/pin/{pinId}` | | `{done: false}`, `{done: false, expired: true, error}`, or `{done: true, servers: [{name, machineIdentifier, version?, owned, alreadyAdded, connections: [{uri, local}]}]}`; 404 for an unknown or expired sign-in |
| `POST /api/media-servers/plex/pin/{pinId}/add` | `{machineIdentifier, uri?}` | the saved server (201, or 200 when it was already saved and got a new token); 409 before the PIN is approved |
| `POST /api/media-servers/jellyfin/quickconnect` | `{baseUrl}` | `{id, code, expiresIn}` |
| `GET /api/media-servers/jellyfin/quickconnect/{id}` | | `{done: false, code}`, `{done: false, expired: true, error}`, or `{done: true, server}`; 404 once used or expired |
| `POST /api/media-servers/login` | `{kind: "jellyfin"\|"emby", baseUrl, username, password}` | the saved server (201, or 200 when already saved) |

Problems worth showing as they are (wrong password, can't reach, Quick Connect off) come back as 400 `{"error": "..."}`. The install's device id sent to plex.tv, Jellyfin and Emby is stored in the `mediaservers.client_id` setting.
