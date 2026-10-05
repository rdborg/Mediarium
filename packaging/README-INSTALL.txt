Mediarium - native program (preview)
====================================

This archive contains the Mediarium program for Linux (mediarium),
the AGPL-3.0 LICENSE, and these notes.

Note: running Mediarium natively on Linux (without Docker) is NOT a
supported install yet. The supported way to install Mediarium is Docker:

  https://github.com/rdborg/Mediarium/blob/main/docs/INSTALL.md

If you still want to try the native program, for testing, here is what you
need to know. Tell us what you find:
https://github.com/rdborg/Mediarium/issues

The web interface is built into the program. RAR and ZIP downloads are
unpacked by Mediarium itself. Two optional helper programs are NOT included:

  par2   (par2cmdline)   verifies and repairs damaged Usenet downloads
  7z     (7-Zip)         only needed for the rare .7z archives

Mediarium looks for them by the plain names "par2" and "7z" on your PATH.

  Debian/Ubuntu : sudo apt install par2 p7zip-full
  Fedora        : sudo dnf install par2cmdline p7zip p7zip-plugins
  Arch          : sudo pacman -S par2cmdline p7zip

Folders
-------
Natively, set these environment variables (the defaults /config, /downloads,
/movies and /tv are meant for Docker):

  CONFIG_DIR      where app.db and secret.key live (back this up)
  DOWNLOADS_DIR   contains incomplete/ and complete/
  MOVIES_DIR      movie library
  TV_DIR          TV library
  MUSIC_DIR       music library (only used if you switch on Music in Settings)
  EBOOKS_DIR      ebooks folder (only used if you switch on Ebooks)
  AUDIOBOOKS_DIR  audiobooks folder (only used if you switch on Audiobooks)
  APP_PORT        web page port (default 8264)

Keep DOWNLOADS_DIR, MOVIES_DIR and TV_DIR on the same drive so imports can
hardlink instead of copy. PUID, PGID and TZ only apply to the Docker image.

Then open http://localhost:8264 and follow the setup wizard.

Restart and updates
-------------------
Settings > System > Server and backup has Restart buttons. They work when
something starts Mediarium again after it exits: systemd is found by
itself (the supplied unit uses Restart=always).
For any other service manager set MEDIARIUM_SUPERVISED=1. Where nothing
would start it again, the page offers Shut down instead.

There is no Update now button for this native program (it is for the
Docker image). To update, download the new version from the release page,
stop Mediarium, replace the program file and start it again. Back up
CONFIG_DIR first.
