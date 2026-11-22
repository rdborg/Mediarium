# Other

The **Other** library is for things that are not movies, shows, music or books: a lecture series, a game soundtrack, home videos, a set of manuals. Nothing in it is looked up on TMDB, TVDB or anywhere else. You make an item by name, and Mediarium keeps its files together in a folder of its own.

It is off on a new install.

## Switching it on

1. Open **Settings > Basics > Media types** and switch on **Other**. The first-run wizard has it too, on the **What do you want to manage?** step, and after an update an administrator is asked once whether to switch it on.
2. Check the **Other folder** under **Settings > Library and files > Folders**. It starts from `OTHER_DIR` (`/other` by default). The folder has to be mapped into the container, the same way as the music or ebooks folder (see [modules.md](./modules.md)).

An **Other** tab then appears in the Library.

## Adding items

Open the **Other** tab and press **Add**. Two ways:

- **By name.** Type a name. The item is empty until you give it files.
- **From a download.** Choose an NZB or .torrent file, or paste an address or a magnet link. Type a name if you like. If you leave it empty, the item is named after the release (dots and underscores between words become spaces).

On an item you can then:

- **Add download** puts another NZB, torrent, address or magnet link into it.
- **Import files** moves a file or folder that is already on the server into the item. The path has to be inside your downloads folder or one of your library folders (a file or folder in them, not the whole folder).
- **Rename** changes the name. The folder keeps its name, and later files go into it as well.
- **Remove** takes the item out of the library, and can delete its folder too (it goes to the recycle bin when that is on). A download still running for it is stopped.

Only administrators add, rename, import and remove. Everyone can look.

You can also add a download to a new or existing item with `POST /api/downloads/manual`, using `otherId` for an item that exists or `other=new` (with an optional `otherName`) for a new one.

## Where files go

Each item gets `<Other folder>/<item name>/`, and its files keep the names they came with. Leftovers such as `.par2`, `.sfv`, `.nzb` and hidden files are not kept. If two files in a download have the same name, the folder they were in is put in front of the name. Characters a file system cannot hold are handled by the same setting as everywhere else (Settings > Library and files > File names and importing).

## Downloads, history and the blocklist

An Other download goes through the same line as every other one: the download client or the built-in downloader, Activity, pause, stop and retry, the duplicate check and the download doctor. A release that fails because it is bad goes on the blocklist. The entry is for the release everywhere, since nothing searches for Other items by itself, and there is no automatic retry.

Media servers are told when files arrive. Plex, Jellyfin and Emby scan the folder only when one of their own libraries holds it, so add the Other folder to a library there (for example "Other videos"). Audiobookshelf and Kavita ignore it.

## What it does not do

Nothing is searched for Other items on your indexers: there is no release list and no automatic search. Items and their downloads are by hand.
