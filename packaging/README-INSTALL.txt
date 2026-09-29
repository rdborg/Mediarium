Mediarium - install notes for this archive
==========================================

This archive contains the Mediarium program (mediarium or mediarium.exe),
the AGPL-3.0 LICENSE, and docs/INSTALL.md, which is the full install guide
(Docker, NAS, Windows, macOS, Linux).

The web interface is built into the program. There is nothing else to install
for the app itself.

Two helper programs are NOT included and are needed for Usenet downloads
that come as RAR/ZIP/7z archives or that need PAR2 repair:

  7z     (7-Zip)         unpacks archives
  par2   (par2cmdline)   verifies and repairs damaged downloads

Mediarium looks for them by the plain names "7z" and "par2" on your PATH.
Torrent-only use does not need them for archive-free releases, but most
Usenet releases do.

  Windows : winget install 7zip.7zip   (then add C:\Program Files\7-Zip to
            your PATH, the 7-Zip installer does not do it), and download a
            par2cmdline build for Windows and put par2.exe on your PATH.
            See docs/INSTALL.md, section "Windows (native)".
  macOS   : brew install p7zip par2     (see docs/INSTALL.md for caveats)
  Debian/Ubuntu : sudo apt install p7zip-full par2
  Fedora        : sudo dnf install p7zip p7zip-plugins par2cmdline
  Arch          : sudo pacman -S p7zip par2cmdline

Folders
-------
Run natively, Mediarium does NOT use /config, /downloads, /movies and /tv
unless you set the environment variables below (those are the defaults
inside the Docker image and they will not exist on Windows or macOS):

  CONFIG_DIR      where app.db and secret.key live (back this up)
  DOWNLOADS_DIR   contains incomplete/ and complete/
  MOVIES_DIR      movie library
  TV_DIR          TV library
  APP_PORT        web UI port (default 8264)

PUID, PGID and TZ are handled by the Docker entrypoint only. A native run
runs as whoever starts it and uses the system timezone.

Keep DOWNLOADS_DIR, MOVIES_DIR and TV_DIR on the same drive/filesystem so
imports can hardlink instead of copy.

Then open http://localhost:8264 and follow the first-run wizard.

Files in this archive marked "UNTESTED" in docs/INSTALL.md (service files,
installer scripts) have not been tried on real machines yet.
