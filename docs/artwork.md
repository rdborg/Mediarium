# Artwork and privacy

Posters, backdrops and the photos of actors and directors come from TMDB. By default Mediarium fetches them for you instead of letting every browser do it. Your browsers only ever talk to Mediarium, and TMDB only ever sees your server.

## What it does

- Each picture is fetched from TMDB's picture host once per size, kept in the `img-cache` folder inside your config folder, and sent from there.
- A page asks for the size it needs. A poster card on a phone gets a picture about 342 pixels wide, not the 2000-pixel original, and a title page backdrop comes in 780 or 1280 pixels wide depending on the screen. The sizes are 92, 154, 185, 342, 500, 780, 1280 and the original. A picture is never made bigger than it is.
- A browser that reads AVIF is sent AVIF, which is the smallest. Every other browser gets JPEG (a transparent PNG logo stays PNG). The first time an AVIF is asked for, the JPEG is sent at once and the AVIF is made in the background, so the next visit gets the smaller file. At most two AVIF files are made at a time, so artwork never takes the server's CPU away from downloads and imports.
- Browsers keep each picture for a week and ask Mediarium whether it changed (`304 Not Modified`) instead of downloading it again.
- Signed-in accounts only. Someone who is not signed in cannot use your server to fetch pictures.

WebP is not made, because Mediarium has no WebP writer. A browser that reads WebP but not AVIF gets JPEG.

## Which pictures can be asked for

Only pictures on TMDB's picture host (`image.tmdb.org`), only at one of the sizes above, only by a plain file name such as `abc123.jpg`. Mediarium never fetches an address a browser sends it, so the picture route cannot be used to reach other machines on your network. The connection to TMDB also refuses private and local addresses, and does not follow redirects. A picture over 15 MB or over 25 million pixels is refused.

A picture TMDB does not have is remembered as missing for ten minutes, and a failed download for one minute, so a page full of broken posters does not hammer TMDB.

## The size of the cache

The cache keeps up to 1 GB. When it grows past that, the pictures nobody has looked at for the longest time go first, down to 90% of the limit. Deleting the `img-cache` folder is safe: pictures are fetched again when a page needs them.

## Switching it off

**Settings > Info, lists and subtitles > Artwork from TMDB > Load artwork through Mediarium.** It is on by default. When you switch it off, every browser loads posters and backdrops from `image.tmdb.org` itself, as in older versions. Pages that are already open change the next time they load. Only administrators can change it.

## What Mediarium contacts

For artwork: your server contacts `image.tmdb.org`. Your browser contacts only Mediarium. With the switch off, the browser contacts `image.tmdb.org` as well, and TMDB then sees your browser's address.

Book and music covers were already fetched by the server and have their own caches (`book-covers`, `music-covers`). Trailers are the one thing a browser still loads from outside, from YouTube, and only if you leave **Play the trailer behind movie and show pages** on. The full list of services Mediarium talks to is in [LEGAL.md](./LEGAL.md).
