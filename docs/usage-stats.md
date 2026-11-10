# Anonymous usage numbers

Mediarium can send one small, anonymous message a day, so the project knows how many installs are in use and **which devices and setups are popular**, and can make Mediarium work better on them. It is **off until an administrator chooses Yes**. If you say No, nothing is ever sent. The totals of everyone who said Yes are **public**, and all of it is in the open source code, so you can check every claim on this page.

## How you are asked

- The first time an administrator opens Mediarium (a new install, or an install that has just been updated), a window asks: **Yes, share anonymous numbers**, **No thanks**, or **Ask me later**. Nothing is chosen for you. **Ask me later** only closes the window until the next time the app is opened.
- The window shows the exact message that would be sent, with the links to the code.
- You can change your answer at any time under **Settings > General > Anonymous usage numbers**. Switching it off forgets the random ID. **Reset my ID** makes a new one, used from the next day's message on (it never sends a second message the same day).
- Accounts that are not administrators are not asked, and do not see the numbers.
- **Never ask, never send:** set the environment variable `MEDIARIUM_DISABLE_STATS=1` on the container. The window is not shown and nothing is sent, whatever was answered before. It is listed in [reference/environment.md](https://github.com/rdborg/Mediarium/blob/main/docs/reference/environment.md).

## What is in the message

One message a day, at most. It is this and nothing else (the code is [`internal/usagestats/usagestats.go`](https://github.com/rdborg/Mediarium/blob/main/internal/usagestats/usagestats.go); a test fails if a field is added without this page changing):

| Field | What it holds |
|---|---|
| `id` | A random 32-character number made on this install when you say Yes. It is not made from anything about you or the machine. It lets one install count once. |
| `version` | The Mediarium version, such as `4.0.3`. |
| `arch`, `docker` | `amd64`, `arm64` or `other`, and whether it runs in a container. |
| `cores`, `memory` | Bands, such as `3-4` cores and `4-8 GB` of memory, never exact figures. |
| `age` | How long the install has existed, as a band such as `1-3 months`. |
| `library`, `accounts`, `indexers` | Bands for how many titles, accounts and indexers there are, such as `101-1000`. |
| `media` | Which kinds of media are switched on: movies, tv, music, ebooks, audiobooks. |
| `features` | Which of a fixed list of features are in use: usenet, torrents, vpn, proxy, debrid, download-client, irc, import-lists, requests, sso, passkeys, kobo, notifications, subtitles. |
| `servers` | Which kinds of media server are connected: plex, jellyfin, emby, audiobookshelf, kavita, silo, webhook. |

Every value is a number band, a yes or no, or a name from these fixed lists. The receiver checks them against the same lists and throws away any message that has anything else.

**Never sent:** your name, titles, folder or file names, host names, web addresses, the names of your indexers, Usenet provider or other services, passwords or keys, anything you typed, or error messages. The app also does not collect anything from your browser: no device, screen or browser details.

## Where it goes and what is kept

- The message goes to `https://mediarium.app/stats/ping.php`. The code that receives it is in [`tools/stats-receiver`](https://github.com/rdborg/Mediarium/tree/main/tools/stats-receiver) and is the same code that runs there.
- It keeps one row per install per day, for 120 days, then deletes it. The row holds only the fields above.
- Like any web server, the server sees the address a message comes from while it arrives. That address is **not saved with the numbers**. To stop one address from sending thousands of fake installs, it makes a one-way scrambled value from the address, with a salt that is replaced every day and the old salts deleted, and keeps it until the day ends. A tool cannot turn it back into an address or follow anyone from one day to the next.
- The totals are shown at <https://mediarium.app/stats/>. A group of fewer than 10 installs is never shown, so nobody can be singled out.
- Nobody outside the project is given the data. It is not sold, and it is not used for advertising.

## Not comfortable sharing from the app?

You can tell us about your setup once, by hand, on the website: <https://mediarium.app/survey/>. Every answer is chosen from a list (what it runs on, processor, memory, system, how it was installed, size, media servers, what it is used for, how you download), plus one optional short note that only the maintainer reads and that is never shown. It asks for no name or email address, and the app is not involved. It does not record your address and does not look up where you are: the country (and what you would like next) is only what you choose to say. It is kept apart from the numbers the app sends, so someone who does both may be counted twice, and nothing links the two. To keep robots out, the page asks the server for a signed one-time token, waits a few seconds and does a small piece of work in your browser (the same limit per address as above applies). The answers are deleted after 24 months. The totals appear on the same public page, marked as self-reported.

## Live numbers about the project

The public page also shows the project's own numbers, read from the public pages of GitHub (stars, forks, the latest release, downloads of release files, open issues) and Docker Hub (pulls and stars), at most once an hour. They count the project, not people, and no personal information is involved. Downloads through the GitHub container registry are not public, so they are not shown.

## Good to know

- Because only people who say Yes are counted, the numbers are always lower than real use. They are for comparing weeks and seeing what kinds of devices people run.
- A message can be faked by anyone with a script. The limits above make large fakes hard, but the numbers are a guide, not a count of every install.
- If you run a fork, `usagestats.URL` is the one place to point it elsewhere or to switch it off in the code.
