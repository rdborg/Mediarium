# IRC announce

Many torrent trackers, private and public, post every new release in an IRC channel a few seconds after it is uploaded. **IRC announce** lets Mediarium sit in that channel, read each announcement and download a release you want straight away. Without it, a new release is found at the next RSS check or search, which can be many minutes later. On a busy tracker, the first seconds are when a release has the most seeders.

You'll find it under **Settings > Finding releases > Indexers & Search**, in the **IRC announce** card below the indexer cards. **It's off until you add a connection and switch it on.** Nothing connects to IRC before that.

## What you need

- An account on the tracker, and its page about IRC. Most trackers explain the network, the channel, the bot that posts and any invite step in their wiki or help pages.
- A **nick** for Mediarium on that network. Many networks only let a registered nick in. You register it once, with an IRC program of your own, following the tracker's instructions.
- Whatever key the tracker gives you for downloads (a passkey, an RSS key, an auth key). It goes into the form and is stored encrypted.
- Torrents switched on (Settings > Downloading > Torrents), and a movie or show in your library that you want.

## What it does with an announcement

An announcement is only ever **checked against your library**. Mediarium never adds a title because an announcement mentioned it.

1. It reads the line with the template you chose (see below) and gets the release name, size and the other details.
2. It looks for a **monitored** movie or show with that name in your library. If there's none, the announcement is skipped.
3. It applies the same rules as the automatic searches and the RSS check: the title and year must match, the release must suit the quality profile (quality, size, language, rejected words), it must not be on the blocklist, the movie must be released, and the title must be **missing** or have a **better version** to gain. The size limits (**Settings > Finding releases > Quality > Size limits**) and the delay profile apply as well: a release outside the size table is skipped, and a release the delay profile holds back is skipped for now, with the time it is let through written in the log. When that time comes, Mediarium searches the title again by itself. A season pack is only taken when the whole season is missing. A release that would repeat what you already have, or one that is only a little different, is skipped.
4. If it passes, the release goes into the download line like any automatic download. It waits behind anything you started yourself, and the usual limits apply (at most 10 automatic downloads an hour, at most 10 waiting).

The same release is never grabbed twice, even if two bots announce it or it is announced again. If the grab fails, that release is not retried.

### Limits

Each connection has two limits you can set, so a bot that posts a flood can't fill your disk:

- **Most downloads an hour** for the whole connection (default 5).
- **Most a day, for each bot** (default 20). Each announcer nick counts on its own.

When a limit is reached the log says "Held back" and says which.

## Adding a connection

Press **Add a connection** and pick the **announcement style** that looks like the lines your tracker's bot posts. If none does, choose **Write my own**.

| Field | What to put |
|---|---|
| **Name** | Anything you like. |
| **IRC server** and **Port** | From the tracker's IRC page. 6697 with the secure connection is the usual. |
| **Use a secure connection (TLS)** | Leave on. **Accept a certificate that can't be checked** is only for a server with a self-signed or expired certificate, and it lets someone in between read your password. |
| **Nick** | The registered nick for the bot, for example `yourname_bot`. |
| **Sign-in** | **NickServ** (a message to NickServ after connecting), **SASL** (signing in while connecting, the better choice when the network offers it) or **No sign-in**. Add the account name only if it isn't the nick. |
| **Invite command** | Some trackers only let you into the channel after you message their bot with a key, for example `BotName announce YOURKEY`. Write the bot's nick, then the message. Separate several with commas. `/sleep 5` waits five seconds. The key in it is stored encrypted. |
| **Channel to join** | For example `#announce`, one per line. If the channel has a password, add it after the name. |
| **Bot that posts the announcements** | The nick of the bot. **Only lines from these nicks are read.** Anyone else in the channel is ignored. |
| **The tracker's web address** | For example `https://tracker.example.org`. Your key is only ever sent to this address. |
| **Only freeleech releases** | Skips anything the announcement doesn't mark as freeleech. |

New connections are saved **off**. Switch one on from its card. The card then says **Connecting**, **Connected, joining the channel** and **Listening**, and tells you what went wrong if it can't. **Show the log** lists the last 50 announcements and what became of each: **Grabbed**, **Skipped** (with the reason in words), **Seen before**, **Not read** (the line didn't fit the template) or **Failed**. Mediarium waits longer before trying again after each failed attempt, and a wrong password or a ban is retried only every 15 minutes, because networks ban bots that keep knocking.

The connection goes through your **proxy** if you set one up for indexers (Settings > Downloading > VPN and proxy). The built-in VPN is for torrent traffic and isn't used for IRC.

## Announcement styles

The built-in styles are written for four common formats. Their patterns follow the way four trackers' announcements are described in the public, MIT-licensed indexer definitions of [autobrr](https://github.com/autobrr/autobrr), which people use for the same job: IPTorrents, TorrentLeech, HDBits and BroadcasTheNet. They are offered as plain styles so you can pick by the shape of the line, and the server, channel and bot names are left for you to fill in from your tracker's own pages. Mediarium doesn't include, recommend or endorse any tracker, and each tracker has its own rules about IRC bots. Read them.

| Style | The line looks like | Keys it needs |
|---|---|---|
| One line: category, name, link and size | `[Movies/HD] Release.Name.2010.1080p.BluRay.x264-GRP FREELEECH - https://tracker.example/details.php?id=1 - 7.63 GB` | `passkey` |
| One line: category, quoted name and uploader | `New Torrent Announcement: <Movies> Name:'Release Name' uploaded by 'user' freeleech - https://tracker.example/torrent/1` | `rsskey` |
| One line: New Torrent, type, uploader and size | `New Torrent: Release Name - Type: Movie (H.264, Encode) - Uploaded by: user - Size: 4.54 GiB - https://tracker.example/details.php?id=1` | `passkey` |
| Several lines for each release | `NOW BROADCASTING! [ Release Name ]`, then a line with the series, one with category and uploader, one with the links | `authkey`, `torrent_pass` |

Your tracker's lines may differ a little from these. Use **Try it** (below) and change the patterns until it reads them.

## Writing your own template

A template has **patterns**, a **download address** and a **size unit**.

**Patterns** are regular expressions, one per line. Name the parts you want to read with `(?P<name>...)`:

| Name | Meaning |
|---|---|
| `releaseName` | The release name. **Required.** |
| `torrentSize` (or `size`) | The size, such as `7.63 GB`, `37 GiB`, `850 MB` or a number of bytes. |
| `category` | The tracker's category, shown in the log. |
| `uploader` | Who uploaded it. |
| `torrentId` | The tracker's number for the torrent, used to build the download address. |
| `freeleech` | Anything here other than empty, `0`, `no` or `false` counts as freeleech. |
| anything else | Can be used in the download address. |

With **Each release is announced over several lines** ticked, the patterns are matched one after another, one line each, and what they capture is put together. A half-finished announcement is dropped after 20 seconds. Without it, each pattern is another way to read a single line, and the first that fits is used.

**Download address** builds the address of the `.torrent` file. Start it with `/` and the tracker's web address is put in front. Write `{{ .name }}` where a value goes: a name your patterns capture is replaced by what the announcement said, and any other name is a **key** (for example `{{ .passkey }}`), which you fill in below and which is stored encrypted. Values are always made safe for use in an address. Example:

```
/download.php?id={{ .torrentId }}&passkey={{ .passkey }}
```

The address that comes out must be on the tracker's own host, whatever the announcement says, so a key can never be sent to another site. An address that starts with `magnet:` is accepted too (for public trackers that announce a hash). For an announcement that carries the full link, `{{ .torrentUrl | raw }}` uses it as it is, but only when it points to the tracker.

**Sizes use** says whether `GB` means 1024 or 1000 megabytes. Most trackers mean 1024.

### Try it

Paste a line (or all the lines of one announcement, in order) into **Try it** and press **Test the line**. It shows how the line was read (release, category, size, uploader, torrent number, freeleech), the **download address with your keys hidden**, and **whether it would be grabbed and why not if it wouldn't**. It grabs nothing, and it works before you've saved or switched on anything. For a connection you've already saved, it also says if a limit is in the way.

## What keeps it safe

- **Only the bots you named are read.** Anyone else in the channel can type anything and it is ignored.
- **Nothing from an announcement is ever sent back** to the IRC server or put into an address that carries a key. The announcement only supplies the release name, the numbers and the text shown in the log.
- Colour codes, bold, other control characters and invisible text are stripped before a line is read. Lines are cut at a fixed length, and a bot that posts hundreds of lines a second is slowed down, so memory stays small.
- **Passwords, the invite command, channel passwords and keys are stored encrypted**, are never sent back to your browser (the page only says whether one is saved) and are never written to the log. If you change the IRC server or the tracker's address, you have to type them again, so a saved secret can't be redirected somewhere new.
- **IRC servers must be on the internet.** Addresses on your own network or the machine itself are refused.
- An announcement can only make Mediarium download something that is **already in your library, monitored and wanted**.
- Turn on safe mode (`MEDIARIUM_PAUSE_AUTOMATION`) and no IRC connection starts.

## Limits

- Torrents only, for movies and shows. Music and books aren't matched.
- A tracker that wants you to sign in to the website to download (instead of using a key in the address) can't be used.
- Mediarium doesn't join channels on its own or follow links in the chat. It only reads the channels you list.
- A private tracker may have rules about how and where you can run an IRC bot, and may ban nicks that join without being invited. Read its rules first.

## For scripts

All of these are for administrators. Passwords, invite commands and keys are never returned.

- `GET /api/irc-announce` lists the connections with their status, and the built-in styles.
- `POST /api/irc-announce` adds one, `PUT /api/irc-announce/{id}` changes it (a blank password, invite command, channel password or key keeps the saved one), `DELETE /api/irc-announce/{id}` removes it.
- `PUT /api/irc-announce/{id}/enabled` with `{"enabled": true}` switches it on or off.
- `GET /api/irc-announce/{id}/log` returns the last 50 announcements.
- `POST /api/irc-announce/test` with `{"template": {...}, "trackerUrl": "...", "lines": ["..."]}` runs the test box.

See [reference/api.md](./reference/api.md). The engine is in `internal/announce`, and `internal/api/irc_announce_decide.go` decides what is wanted. Its tests run against a small IRC server written inside the test.
