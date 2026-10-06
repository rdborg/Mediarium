# Running a script after each import

Mediarium can run a script of your own each time it imports something: a movie, episodes, an album, an ebook or an audiobook, and also when a better version replaces one you had. Use it for extra steps Mediarium doesn't do itself, such as telling another app, copying a file somewhere, or writing a line to your own log.

It is off until you pick a script.

## Setting it up

1. Put the script in the `scripts` folder inside your config folder. In the container that is `/config/scripts`; on the host it is the folder you mapped to `/config`, for example `/volume1/docker/mediarium/config/scripts` on a Synology. Mediarium creates the folder the first time you open the page.
2. Make it executable: `chmod +x /path/to/config/scripts/myscript.sh`. It must also be readable by the user Mediarium runs as (`PUID`/`PGID`).
3. Start it with a line naming what runs it. The container has a plain shell, so `#!/bin/sh` works. It has no Python or Bash; a script that needs them won't start.
4. In Mediarium go to **Settings > System > Run a script after each import**, pick the script, and press **Save**. **Try it** runs it once straight away with a made-up movie, so you can see that it works.

Only files in that folder can be picked, and only from that page while signed in. An API key can't choose or run a script, because choosing what runs is the same as being able to run anything as Mediarium's user. Files whose name starts with a dot, folders, and links that lead out of the folder are not offered.

## What the script is told

Everything comes in environment variables. A variable is left out when there's nothing to put in it.

| Variable | What it holds |
|---|---|
| `MEDIARIUM_EVENT` | `imported`, or `test` for **Try it** |
| `MEDIARIUM_MEDIA` | `movie`, `episode`, `show` (a season pack), `album`, `ebook` or `audiobook` |
| `MEDIARIUM_TITLE` | The movie, show or book title; for music "Artist – Album" |
| `MEDIARIUM_YEAR` | The year, when known |
| `MEDIARIUM_EPISODE` | For TV: `S02E03`, `S02E03-E04`, or `Season 2` for a season pack |
| `MEDIARIUM_QUALITY` | The quality, for example `Bluray-1080p` or `FLAC` |
| `MEDIARIUM_PATH` | Where the file was saved, or the folder when several files came in together (a season pack, an album) |
| `MEDIARIUM_FOLDER` | The folder the files are in |
| `MEDIARIUM_RELEASE` | The name of the release that was downloaded |
| `MEDIARIUM_SIZE` | The size in bytes |
| `MEDIARIUM_PAGE` | The title's page in Mediarium, for example `/title/597` |

Besides these it gets only `PATH`, `HOME`, `TZ` and `LANG`. Nothing else from Mediarium's own environment reaches it. It starts in the scripts folder.

A small example that keeps a list of everything imported:

```sh
#!/bin/sh
echo "$(date '+%Y-%m-%d %H:%M') $MEDIARIUM_MEDIA: $MEDIARIUM_TITLE $MEDIARIUM_EPISODE -> $MEDIARIUM_PATH" >> imported.log
```

## How it runs

- One run at a time. When several imports finish together, their runs wait their turn. More than 50 waiting are dropped (and logged).
- A run is stopped after the time you set (5 minutes unless changed, at most an hour), together with anything it started. Anything it leaves running in the background is stopped when it finishes, so a script can't leave programs behind. **Try it** stops after a minute at most, and says so if a run after an import is still going.
- The import never waits for the script and doesn't depend on it.
- If a run fails (it can't start, ends with an exit code other than 0, or is stopped), the page shows what went wrong with the last few lines it printed, and a line is written to Activity. A run that went fine shows only on the page.

## Turning it off

Pick **None (off)** and press **Save**. Removing the file from the folder also stops it; the page then shows it as missing.

## For developers

- `GET /api/scripts` (administrators): `folder`, `scripts` (the executable files in it), `script`, `timeoutSec` and `lastRun`.
- `PUT /api/scripts` with `{"script": "myscript.sh", "timeoutSec": 300}`; an empty `script` turns it off. `POST /api/scripts/test` runs it once. Both answer 403 to an API key.
- Stored as `scripts.after_import` and `scripts.last_run`.
