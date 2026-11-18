# Ask your library questions from a chat or a voice assistant (MCP)

Mediarium can talk to chat programs and voice assistants that support MCP, the Model Context Protocol. Once one is connected you can ask things like:

- "Which of my movies are not FuN releases yet?"
- "How many movies are over 15 GB?"
- "Which downloads failed this week? Try another release for the first one."
- "Show the latest requests from Sam."
- "Move Tide Pool to the 4K profile."
- "Add the show with TVDB id 123."
- "Find a show like Lupin, add it, and download the first season once I say so." (needs [Let assistants pick releases and start downloads](#let-an-assistant-start-downloads) switched on)

Questions are answered straight away. Anything that changes something is shown first and only happens after you say yes.

It is off until you switch it on, and it adds nothing to install: it is part of Mediarium.

## Switch it on

1. Open **Settings > Media servers and apps > Other apps**.
2. Under **Let AI assistants connect (MCP)** switch it on. The address appears with a copy button.
3. Make an API key for the assistant: press **Make a key for other apps** on the same page, or use **Settings > People and access > Security > API keys**. For a key that may only ask questions, tick **Read-only** when you make it.

Only an administrator's key works. A key of a basic account is refused. (Programs that can sign in by themselves don't need a key at all, see [Sign in instead of a key](#sign-in-instead-of-a-key).) Switching MCP **on** can only be done from the web page while you are signed in, never with an API key, so a leaked key can't open the door by itself.

## Connect your assistant

The address is `http://<mediarium>:8264/mcp` (use your own host and port; behind a reverse proxy with `URL_BASE`, put that path in front: `https://home.example.com/mediarium/mcp`). Mediarium also answers at `/api/mcp`. The key goes in the `Authorization` header as `Bearer <your key>`. The `X-Api-Key` header works too.

Most programs that support MCP over the web take a block like this in their settings file:

```json
{
  "mcpServers": {
    "mediarium": {
      "url": "http://192.168.1.20:8264/mcp",
      "headers": { "Authorization": "Bearer YOUR_API_KEY" }
    }
  }
}
```

Some programs use a `servers` block and want the kind spelled out:

```json
{
  "servers": {
    "mediarium": {
      "type": "http",
      "url": "http://192.168.1.20:8264/mcp",
      "headers": { "Authorization": "Bearer YOUR_API_KEY" }
    }
  }
}
```

A program that can only start a local command, not call an address, can go through the small `mcp-remote` bridge:

```json
{
  "mcpServers": {
    "mediarium": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://192.168.1.20:8264/mcp", "--header", "Authorization: Bearer YOUR_API_KEY", "--allow-http"]
    }
  }
}
```

**Home Assistant** and programs that only know the sign-in (no key field): see the next sections.

To try it without any assistant, ask Mediarium directly:

```
curl -s http://192.168.1.20:8264/mcp \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"library_stats","arguments":{}}}'
```

## Sign in instead of a key

Some programs can't be given a key. They look for a sign-in by themselves, the way "Sign in with Google" works, and then ask you to approve in the browser. Mediarium supports that (OAuth 2.1 with PKCE, as the MCP specification describes it). It works next to the keys, which keep working exactly as before.

What happens: the program asks the MCP address, is told where the sign-in is, opens a Mediarium page in your browser, you sign in the usual way (password, single sign-on, passkey) and press **Allow**. The program then gets its own tokens. Only an administrator can press Allow. On that page you can also untick **Let it make changes too** so the program can only ask questions. A token that only reads is treated like a read-only key.

Tokens last an hour and renew themselves quietly for 90 days of use. They belong to the administrator who approved. They stop working when that person is made a member, switched off, removed, or changes their password, and when you disconnect the app.

You see and end all of this on **Settings > Media servers and apps > Other apps**, under **Connected apps**: the program's name, the account, what it may do, when it connected and when it was last used, with a **Revoke** button.

### Home Assistant

Home Assistant's *Model Context Protocol* integration only speaks the sign-in. There are two ways, depending on what your version asks for.

**Automatic (nothing to copy):**

1. In Mediarium, switch MCP on (see above).
2. In Home Assistant open **Settings > Devices & services > Add integration**, pick **Model Context Protocol** and enter the address, for example `https://mediarium.example.com/mediarium/mcp`. Use the same address you open Mediarium with from your browser. Home Assistant has to be able to reach it too.
3. Home Assistant sends you to Mediarium in your browser. Sign in as an administrator and press **Allow**.
4. Back in Home Assistant, finish the dialog and pick the Mediarium tools for your voice assistant.

**With application credentials** (when Home Assistant asks for a client ID and a client secret first):

1. In Mediarium open **Settings > Media servers and apps > Other apps**, then **Register a client for apps that ask for a client ID and secret**.
2. Enter the name `Home Assistant` and the redirect address `https://my.home-assistant.io/redirect/oauth`. If you use Home Assistant without My Home Assistant, use the redirect address your version shows (it ends in `/auth/external/callback`; plain `http` is fine for this one because you entered it yourself). Press the button and copy the client ID and the secret. The secret is shown once.
3. In Home Assistant open **Settings > Devices & services**, the three dots at the top right, **Application credentials**, **Add application credentials**. Pick **Model Context Protocol** and paste the ID and the secret.
4. Add the **Model Context Protocol** integration with the Mediarium address and approve in the browser as above.

If it can't find the sign-in, open `https://<your address>/.well-known/oauth-authorization-server` in a browser. It should show a short block of text with `issuer` and `token_endpoint`. If you see a page or nothing at all, the address Home Assistant uses doesn't reach Mediarium, or MCP is off. Behind a reverse proxy, pass on the `Host` and `X-Forwarded-Proto` headers so Mediarium knows its public address. A proxy on another machine also has to be listed in `TRUSTED_PROXIES` (see [security](./security.md#trusted_proxies)). If Home Assistant still can't, use the API key route above: it keeps working.

### Other clients

Programs that support the MCP sign-in (VS Code, MCP Inspector and others) need only the address of Mediarium's MCP endpoint. They read the rest from these places and register themselves:

| What | Address |
|---|---|
| MCP endpoint | `https://<your address>/mcp` |
| Protected resource information | `/.well-known/oauth-protected-resource` |
| Sign-in server information | `/.well-known/oauth-authorization-server` |
| Registration | `POST /oauth/register` (public clients; redirect addresses must be `https`, `http` on localhost, or the app's own scheme) |
| Sign in | `GET /oauth/authorize` (code flow, PKCE `S256` required, scopes `mcp:read` and `mcp:write`) |
| Tokens | `POST /oauth/token` (`authorization_code` and `refresh_token`) |

Behind `URL_BASE` put the prefix in front of all of them. A program that asks for a client ID and secret (and doesn't register itself) uses a client from **Register a client** on the Other apps page. Such a client with a secret may leave out PKCE; one without a secret may not.


**Questions** (any administrator key, read-only ones too):

| Tool | What it answers |
|---|---|
| `library_search` | Find titles. Filters: query (matches the title, the genres and the description, title matches first), genre (Action, Comedy, Sci-Fi and so on), kind (movies, tv, music, books; singular and other everyday names such as film, series or shows work too), quality, smallest and largest size in GB, release group, audio language, missing subtitle language and video codec (from the tracks inside the files, and for subtitles also the subtitle files beside them), incomplete (only titles with a file far shorter than the movie or episode should be), has a file, monitored. Gives the total and a page of results (up to 100), so it also counts: "how many movies over 15 GB" is one call. |
| `library_stats` | Totals, space used, split by quality, movies by file size and the most common release groups. |
| `lookup_title` | Looks a movie or show up by name to get its TMDB id. |
| `movie_details`, `series_details` | One title: file, quality, size, release group and release name; for shows the seasons and the aired episodes that are missing. |
| `wanted_list` | What is still being looked for, or still below the quality the profile wants. |
| `queue_status` | The download queue with progress. |
| `recent_history` | The newest lines of Activity, optionally only the problems. |
| `downloads_failed` | Failed downloads with the reason. |
| `health_report` | The same health items as the dashboard. |
| `requests_list` | Requests from the request portal, by person, status or kind. |
| `profiles_list` | Quality profiles with their qualities (and their own order and groups, when they have one), size limit, preferred terms and custom formats. |
| `release_group_report` | The titles that are **not** from a given release group, for movies or TV. |

Sizes are in GB of 1024 MB, the way file managers show them. The release group comes from the release name; for titles that were imported from an existing library it is read from the file name, and titles where it can't be told are counted as unknown.

**Changes** (an administrator's key that is not read-only):

| Tool | What it does |
|---|---|
| `add_movie` | Adds a movie by TMDB id, on a profile you name, and searches for it. Needs [the downloads switch](#let-an-assistant-start-downloads). |
| `add_series` | Adds a show by TMDB or TVDB id, with the monitoring you choose. Needs the downloads switch. |
| `search_now` | Searches right now for up to 20 movies, shows or episodes. Needs the downloads switch. |
| `set_movie_profile`, `set_series_profile` | Moves a title to another quality profile ("upgrade it to 4K"). If its file is below the new profile, a search for a better one starts. |
| `retry_failed_download` | Blocks the release of a failed download and searches for another. |
| `add_preferred_term` | Adds a preferred term with a score to a profile, or changes the score of one that is there. |

Each of these needs `confirm: true`. Called without it, the tool changes nothing and answers with what it would do, and tells the assistant to ask you. Only when you agree does the assistant call it again with `confirm` set. The changes run through the same code as the web pages, so the same checks apply (a title that is already in the library is refused, a profile that doesn't exist is refused, and so on).

### Let an assistant start downloads

Picking one specific release and downloading it is a separate permission, and it is **off** until you switch it on. On **Settings > Media servers and apps > Other apps**, under **Let AI assistants connect (MCP)**, switch on **Let assistants pick releases and start downloads**. Like the main MCP switch it can only be turned on from the web page while you are signed in, never with an API key. With it off, the tools below don't even show up in the assistant's list, and calling them anyway is refused with a plain message. That also goes for `add_movie`, `add_series` and `search_now` from the table above: adding a title or searching for it starts a search that downloads by itself, so those three need the same switch. Everything else in the change table still works with the switch off.

| Tool | What it does |
|---|---|
| `find_releases` | Searches your indexers for releases of a title that is in the library (a movie, or a season, an episode or the packs of a show) and lists the best matches with size, quality, group and anything that speaks against them. Each one gets a short `release_id`, and says when the tracker counts it as freeleech (`freeleech`) or at a discount (`discount_percent`, 50 is half price). Nothing is downloaded. The download address stays inside Mediarium and is never given to the assistant. |
| `grab_release` | Downloads one of those releases. It takes two calls (see below). |

Both need a key that is not read-only (or a sign-in that was allowed to make changes), next to the switch.

How the two calls work:

1. The assistant calls `grab_release` with only the `release_id`. Nothing happens. Mediarium answers with what would be downloaded (release name, size, which title) and a `confirm_token`.
2. The assistant tells you and asks. Only if you say yes does it call `grab_release` again with the same `release_id` and the `confirm_token`. Now the release goes into the download queue, through the same code as pressing **Download** on a release in the web app.

The token works **once**, only for that release and that account, and runs out after 5 minutes. A made-up, used, expired or mixed-up token downloads nothing and is used up by trying it. A newer preview of the same release replaces the older token. A release id is kept for 30 minutes, then the assistant has to search again. If you switch the setting off between the two calls, the second one is refused. Tokens and release ids live in memory only, so a restart of Mediarium clears them.

A whole request such as "find a show like Lupin and download it" then goes: the assistant suggests titles, you pick one, `add_series` previews and adds it (after your yes), `find_releases` lists releases, `grab_release` previews one, and the download starts after your second yes.

Besides the tools, Mediarium offers two resources (a library summary and the health items) and three ready-made prompts (a health check, a look at failed downloads, and a release group check). Programs that show prompts list them.

## Good to know

- **The yes is the assistant's to give.** Mediarium can't see whether you really said yes in the chat; it only sees `confirm: true` in the call, or the `confirm_token` of a download. The token makes it a deliberate second step (a program that never asks can't start a download by accident or by one stray call), but it can't prove that a person agreed. Keep **Let assistants pick releases and start downloads** off unless you trust the program. An assistant that is told to, or tricked into it by text it reads, could send it without asking. If you don't fully trust a program, give it a **read-only key**: it then only sees the question tools and can't change anything.
- **Use HTTPS when you can.** Keys and tokens travel in a header and the sign-in sends the browser back with a one-time code. Over plain `http` on your home network that is fine for many people; across the internet put Mediarium behind HTTPS (a reverse proxy).
- **Tokens are revocable.** Each connected app can be disconnected on the Other apps page, and it stops at once. Mediarium keeps only a hash of every token, so a copy of its database doesn't give them away. A refresh token that is used a second time is treated as stolen and ends the whole connection. The one-time code is valid for 60 seconds.
- **A registered client's name is what the registering program called itself.** The Allow page therefore also shows the address the browser goes to afterwards. If the name and the address don't fit together, press Deny.
- **Make a key just for it**, named after the program. Revoke it under **Settings > People and access > Security > API keys** and it stops working at once. Switch MCP off on the Other apps page and the address stops answering (404) for every key.
- **Your library leaves the house.** What a tool answers (titles, file names, sizes, request names) goes to the assistant, and from there to whoever runs it. A voice assistant or chat program that runs fully on your own machine keeps it at home; a hosted one does not.
- **Limits.** A request can be up to 256 KB; one key can send about 20 requests a second (with a burst of 100) before it is asked to wait; a long answer is cut and says so. Nothing is kept between requests (no sessions).
- **Mistakes come back as plain answers.** If a tool can't do what was asked (an unknown kind, an argument that isn't a number, a search that took too long, an unexpected crash) it answers with a short explanation marked as an error, never with an empty reply or a failed connection. Numbers and yes/no values sent as text ("5", "true") are accepted. A search that finds nothing says "No titles matched" and how many titles the library holds. If an assistant still reports an empty "error communicating with the MCP server", look for a `mcp: a tool was slow` line in the log: the call took longer than the assistant waited.
- **The log** has one line per tool call with the tool's name, how it went and how big the answer was. It never has the arguments or the key.
- **Not here yet:** music and books can be searched by name, but they have no change tools, and there is no way to delete anything.
