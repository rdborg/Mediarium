# Channel archiver

The Channel archiver follows YouTube channels and playlists and keeps their videos in a folder of your own, or just the sound if you want music. It is **off** until you switch it on under **Settings > Media types**, and it needs a copy of **yt-dlp** that you supply.

## Read this first

Downloading from YouTube may break YouTube's terms of service. You are responsible for what you download and for having the right to keep it. Mediarium does not include yt-dlp, does not download it and does not update it. The module can only be switched on after you tick a box that says you have read this.

## Setting it up

1. **Get yt-dlp yourself** from [its own project](https://github.com/yt-dlp/yt-dlp). Audio only (mp3, m4a) and merging video need `ffmpeg` too, on the same machine or in the same container.
2. Under **Settings > Media types**, the **Channel archiver** card has these fields:
   - **The yt-dlp file**: the full path, for example `/usr/local/bin/yt-dlp`. **Test** runs `yt-dlp --version` and shows the version it prints.
   - **Download folder**: where the videos go. Mediarium makes it if it is missing and checks that it can write there.
   - **Format** for new channels: **Video, up to 1080p** (mp4), **Audio only, mp3** or **Audio only, m4a**. Each channel can have its own.
   - **Videos per check**: the most videos downloaded for one channel in one check (5 at first, 1 to 50).
   - **Pause between downloads** in seconds (30 at first, 0 to 3600).
   - **Check every** that many hours (12 at first, 1 to 168).
   - **Longest for one video** in minutes (60 at first, 5 to 720). A download that takes longer is stopped.
   - **Other websites**: YouTube is allowed by default (`youtube.com`, `www.youtube.com`, `m.youtube.com`, `music.youtube.com`, `youtu.be`). Any other website yt-dlp can read has to be added here by name first, for example `vimeo.com`. Only plain website names are accepted, not addresses or IP numbers.
3. Tick **I have read this** under the warning, press **Save**, then switch **Channel archiver** on. If the box isn't ticked, there is no folder, or the yt-dlp file doesn't answer with a version, the switch refuses and says which.

There is no setting for a cookies file, so videos that need a sign-in can't be fetched.

### yt-dlp in Docker

Mediarium's image doesn't have yt-dlp. Put the file next to your other config and mount it read-only:

```yaml
    volumes:
      - ./yt-dlp:/usr/local/bin/yt-dlp:ro
      - /srv/archive:/archive
```

Then set **The yt-dlp file** to `/usr/local/bin/yt-dlp` and **Download folder** to `/archive`. The file has to run inside the container, so use the standalone Linux build that matches it (`yt-dlp_linux` or `yt-dlp_linux_aarch64`) and, for audio or merged video, a build of ffmpeg that is also in the container. Update the file yourself when YouTube changes something and downloads start to fail.

## Following a channel

In the card, **Follow** takes the address of a channel (`https://www.youtube.com/@name`) or a playlist, an optional folder name and a format. A channel address gets `/videos` added so the list holds videos and not the channel's tabs. Only `https` addresses on the allowed websites are accepted.

- A new channel starts from now: the videos already there are noted as seen and not downloaded. Tick **Also download the videos already there** to get the backlog too, a few at a time (the newest 200 videos of a channel are looked at).
- The table shows each channel with its format, when it was last checked, how many videos are downloaded, waiting and failed, and what went wrong. **Check now** checks it straight away and tries failed videos again. **Remove** stops following it and leaves the files where they are.
- Below it, the last 30 videos that were downloaded or failed, with the reason.

## How it runs

Every ten minutes a background job picks the channel that has waited longest, if its **Check every** time has passed, and checks it. One channel at a time, and nothing is checked in the first ten minutes after Mediarium starts. A check asks yt-dlp for the list of the newest videos (it downloads none), remembers every video id it has seen, and downloads the new ones one after another, newest first, up to **Videos per check**, waiting the pause between two. A video that fails is tried again at the next checks, three times in all.

Files go to `<download folder>/<channel name>/<title> [<video id>].<ext>`, with the `.info.json` yt-dlp wrote next to it. The title only ever becomes part of a file name after slashes and other odd characters are taken out. While a video downloads it sits in a hidden `.archiver-...` folder inside the download folder, which is cleaned up afterwards.

Switching the module off stops a running check after the video it is on and keeps your channels, the list of seen videos and the files. Nothing is checked in safe mode.

## How it keeps yt-dlp in check

- yt-dlp is started directly with a fixed list of options (never through a shell), always with `--ignore-config`, and the address always comes after `--`, so it can never be read as an option.
- Addresses have to be `https` on an allowed website, with no user name or port.
- File names don't come from yt-dlp's `-o` templates with text from the video: yt-dlp writes `<id>.<ext>` and Mediarium renames it.
- Each download has a time limit, and stops when you switch the module off or Mediarium stops.
- yt-dlp gets a small environment (no Mediarium keys), and only the last few lines of what it says are kept, shown in the list when a video fails.

## Left out

- Music mode doesn't file audio into the music library. Audio stays in the download folder, under the channel's name, for you to move or point your player at.
- Videos aren't shown in Activity. The card has its own list.

## For scripts

All of these are for administrators.

| Route | What |
|---|---|
| `GET /api/archiver` | settings, followed channels and the last videos |
| `PUT /api/archiver` | save the settings (`ytdlpPath`, `folder`, `format`, `maxPerRun`, `pauseSeconds`, `intervalHours`, `timeoutMinutes`, `extraHosts`, `acknowledged`) |
| `POST /api/archiver/test` | `{"path": "..."}` runs `--version` and answers `{"version": "..."}` |
| `POST /api/archiver/sources` | `{"url", "name", "format", "backlog"}` follows a channel or playlist |
| `DELETE /api/archiver/sources/{id}` | stop following |
| `POST /api/archiver/sources/{id}/check` | check now, in the background |

The switch is `archiver` in `PUT /api/modules`. The settings are `modules.archiver` and `archiver.config` (see the [settings keys](./reference/settings-keys.md)).

The idea came from SanketBaraiya (#143); see the [community page](./community.md).
