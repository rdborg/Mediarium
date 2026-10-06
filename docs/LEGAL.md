# Legal notice and disclaimer

> **This page is a plain-language notice written by the project, not legal advice.** See the last section.

## What Mediarium is

Mediarium is a general-purpose automation and organization tool for media files. It talks to services that **you** configure (metadata sites, search indexers, Usenet providers, torrent networks, subtitle sites, VPN providers), and it renames and files what you download into your own library folders.

## What Mediarium is not

- Mediarium does **not** host, store, index, upload, stream or provide any media content.
- Mediarium does **not** ship with any indexer, tracker or content source, and does not recommend or endorse any. You add your own accounts. Where the forms offer a short list of names (a few well-known indexers, Usenet providers' server addresses, or the community list of torrent sites), it is only there to save you typing, and being on it is not an endorsement.
- Mediarium is **not** a service. There is no Mediarium server that handles your downloads or sees your library. The software runs on your own hardware.

## Your responsibility

You alone are responsible for:

- what you search for, download, store, share or seed with it;
- complying with the copyright and other laws of the country you live in (and any other country that applies to you);
- following the terms of service of every provider you connect (your Usenet provider, indexers, VPN provider, TMDB, OpenSubtitles, Trakt and so on).

Mediarium is intended for content you have the legal right to obtain, for example public-domain works, backups of media you own, and freely or openly licensed content. Where the law in your country does not allow you to download or copy something, do not use Mediarium for it.

Torrent traffic exposes your IP address to other people in the swarm. The optional built-in VPN feature is a privacy tool, not a legal shield, and it is not a substitute for making lawful choices.

## No affiliation, third-party names

Mediarium is an independent project. It is **not** affiliated with, sponsored by or endorsed by TMDB, Trakt, OpenSubtitles, or any indexer, Usenet provider, VPN provider, or any other service or company it can connect to. Radarr, Sonarr, Prowlarr, SABnzbd, Bazarr, Plex, Jellyfin, Emby, Synology, Unraid, QNAP and all other product names, logos and trademarks mentioned in this repository belong to their respective owners and are used only to describe compatibility or comparison.

### TMDB attribution

> This product uses the TMDB API but is not endorsed or certified by TMDB.

Movie and show metadata and images come from [The Movie Database (TMDB)](https://www.themoviedb.org/). The in-app About page carries this notice and the TMDB logo. TMDB's own terms of use decide exactly how attribution must look; check their current terms if you redistribute or modify the app.

### Other services

Subtitles come from OpenSubtitles and public list import from Trakt, each under their own terms and API rules. **Sign in with Plex** talks to plex.tv, only when you use it. The list of torrent sites comes from the community [Prowlarr/Indexers](https://github.com/Prowlarr/Indexers) definitions, which your server downloads when you open the site list. Music information comes from [MusicBrainz](https://musicbrainz.org) (open data, CC0), album covers from the [Cover Art Archive](https://coverartarchive.org) and the popular and new-release lists from [ListenBrainz](https://listenbrainz.org). Book details, series and covers come from [Open Library](https://openlibrary.org), a project of the Internet Archive. Audiobook narrators and running times come from [Audnexus](https://audnex.us), which reads Audible's catalogue, after Audible's public catalogue search has found the book. With your own token, [Hardcover](https://hardcover.app) adds more series and release dates; it is only contacted when you save one. The ebook reader uses [epub.js](https://github.com/futurepress/epub.js) (BSD-2-Clause). All are credited on the in-app About page. Once a day Mediarium asks GitHub for the newest release and sends only its own name and version. You can switch this off under **Settings > System > Server and backup**. An official build may contain the maintainer's own API identifiers for these services. If you build from source, you use your own.

## License and warranty

Mediarium is free software under the [GNU Affero General Public License v3.0](../LICENSE). The software is provided **"as is", without warranty of any kind**, express or implied, and the authors and contributors are not liable for any claim, damages or other liability arising from its use. The license text has the exact terms. Back up your data: Mediarium can rename, move and (when you ask it to) delete files.

## What this notice cannot do

A disclaimer like this one doesn't, by itself, protect the maintainer, contributors or users from legal risk. Whether a tool that automates downloading is lawful depends on the country and on facts such as how the project is described, what it links to and what it ships with. A disclaimer doesn't bind courts, rights holders or hosting platforms.

This page is written by the project. It is not legal advice, and no lawyer has reviewed it. If you distribute Mediarium, run it as a service for other people, or add links or payments to a version of your own, talk to a lawyer who knows software and copyright law where you live.
