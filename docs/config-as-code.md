# Settings file with a check

Settings > System has a **Settings file** card. It writes your setup as one text file, and it puts a file like that back with a check first: you see what would be added, changed or removed before anything is written. Use it to keep your setup in git, to move to a new install, or to see exactly what changed between two files.

The file is YAML and is the same one the command line uses (see [Keep your settings in a file](config-file.md)), so a file from either place works in the other.

## Download the file

**Download settings file** writes everything that is not a secret:

- the settings of every Settings page that is one set of values (folders, naming presets, schedules, languages, subtitles and so on), the torrent client, the debrid service, the proxy and the update options
- indexers, Usenet servers and media servers
- quality profiles, with the profiles they fall back to and their custom format scores named, not numbered
- notification channels

Accounts, passwords, sign-in settings, your library and the history are not in it. Custom formats themselves, import lists and the switches for media types are not in it yet either.

The file starts with `formatVersion: 1`, lists are sorted by name and the keys are in a fixed order, so two downloads of the same setup are identical and a diff in git shows only what changed.

Two settings point at a quality profile by its number, `defaultProfileId` and `defaultTvProfileId`. The numbers belong to the Mediarium the file came from, so when you restore onto a different install, check on the Quality page that the default profiles are the ones you expect.

### Passwords and keys

A password or key is never written into the file. Where Mediarium has one saved, the file says `password: <secret>` so you can see there is one. Read back, `<secret>` means "keep the one that is saved".

To restore onto a new install, the file adds the Usenet server, indexer or notification channel without its password, and the check lists which ones still need a password. Enter those on the Settings page afterwards. You can also write the real password in the file, or `${NAME}` when you apply it at start (see below). A file you choose on the page can't read environment variables, so there the value has to be written in.

## Check this file

Choose a file and press **Check this file**. Nothing is written. Mediarium reads the file and shows:

- **Added**: items the file has and Mediarium doesn't
- **Changed**: what differs, one line per value (`trashDays: 7 -> 14`)
- **Unchanged**: how many items already match
- **Removed**: only in replace mode, see below
- **Passwords still needed**: new items whose password is not in the file

The check stops the file, with a plain reason, when:

- it isn't YAML or JSON, or has nothing in it
- it was made by a newer Mediarium (`formatVersion` higher than this one reads). Update Mediarium first
- a name isn't one Mediarium has (a typo in a setting)
- a value is the wrong kind: text where a number goes, or something other than true or false
- an address (`url`, `baseUrl`, `publicUrl`) doesn't start with `http://` or `https://`
- a quality profile falls back to a profile, or scores a custom format, that is neither in Mediarium nor in the file

The limits of each setting (a number that has to be between 1 and 50, a folder that has to exist) are Mediarium's own rules, and they run when you apply. If one of them refuses a value, nothing is kept (see below).

### Replace mode

Normally a file only adds and changes: an item that is in Mediarium but not in the file stays. Tick **Replace what the file lists** to also remove, from each list the file has, the items it doesn't mention. The check lists each one as **Removed**. A list the file doesn't have at all is never touched, and settings missing from the file are never reset.

Removing is the one part that can't be taken back completely: a removed indexer or server comes back without its password. Mediarium keeps a copy of the database first (below). It also still refuses some removals, such as the default quality profile, and then the whole apply is undone.

## Apply

**Apply** is only there once the check found something to do and no problems. It applies the file you checked: if you change the file after checking, check again.

1. Mediarium saves a copy of the database, `pre-config-<date and time>.db`, in your config folder next to the database. The newest three are kept.
2. It makes the changes one after another, through the same routes the Settings pages use, so every value passes the same rules as when you type it in.
3. If Mediarium refuses something halfway, everything already done is put back and you are told which item was refused and why. Nothing stays half-applied.

One thing can't be put back: a password or key the file set. Mediarium never shows passwords, so it can't know the old one. If that matters the result says how many there were. The copy of the database from step 1 is what you would restore from, with Mediarium stopped (replace `app.db` with the copy). The usual **Restore** on this page works with backups, not with these copies.

## Apply the file in the config folder at start

If you keep the file next to your compose file, Mediarium can apply it for you. This is off until you switch on **Apply the settings file at start** on the card. It only takes the switch from a signed-in browser, not from an API key.

With the switch on, at every start Mediarium looks in the config folder for `mediarium-config.yaml`, `mediarium-config.yml` or `mediarium-config.json`. If the file is different from the one it applied last time, it checks it and applies it the same way as above, with a copy of the database first. Details:

- A file that hasn't changed is not applied again, so settings you change by hand afterwards are not overwritten at the next start.
- It only adds and changes. It never removes anything.
- `${NAME}` in this file is read from the environment of the Mediarium container, so passwords can stay out of the file.
- It never stops Mediarium from starting. If the file has a problem, nothing is changed, the reason is written to the log and shown on the card, and the same file is not tried again until you change it.
- Nothing is applied while there is no administrator account yet.

## For scripts

| Address | What it does |
|---|---|
| `GET /api/system/config-file` | the settings file |
| `POST /api/system/config-file/check` | the file as the body (`?replace=1` for replace mode). Answers `review` and a `hash` |
| `POST /api/system/config-file/apply` | the same body, with `?hash=` from the check. Signed-in browser only |
| `GET`, `PUT /api/system/config-file/start` | the switch for applying at start, and what the last apply at start did |

All of them need an administrator.
