# Languages

Mediarium's screens come in **English, Spanish (Español), German (Deutsch), French (Français), Italian (Italiano), Dutch (Nederlands), Polish (Polski), Brazilian Portuguese (Português do Brasil), European Portuguese (Português de Portugal), Russian (Русский) and Ukrainian (Українська)**.

## Choosing a language

- **Settings > General > App language** changes it for the browser you're using, and so does **App language** in the profile menu. The page reloads in the new language. It only changes the language of the app itself, not the audio or subtitle language.
- The sign-in page and the first step of setup have the same list, so you can switch before signing in.
- Until you pick one, Mediarium follows your browser's language when it has a translation, and English otherwise. A browser set to Portuguese from Portugal gets the European Portuguese translation, any other Portuguese the Brazilian one.

The choice is kept per browser, so a phone and a laptop can each use their own language. Dates and numbers follow the language too.

## Titles and descriptions in your language

The language for movie and show information is its own choice, apart from the screen language: someone can read Mediarium in English and still want film titles in Portuguese.

- Pick it under **Settings > Info, lists and subtitles > Movie and show info > Language for movie and show info**, or in the **Connect your services** step of setup. There are about thirty languages. If you've set a screen language, a button offers the matching one.
- Release dates on a movie page and in the calendar are the ones of a country. Under **Country for release dates**, in the same card, you can pick yours (India, say, while reading in English); **Same as the info language** takes it from the language above, so English means the US.
- It changes the titles, descriptions, genres and posters on **Discover, search results and title pages**. Where TMDB has no translation of something, you see the English text.
- The same language is used for the **title and poster of a movie or show already in your library**: the Library, the Dashboard, Activity (a download and the waiting list), the calendar, reports, requests, statistics, and the title and poster in a notification. Where TMDB has no translation or no poster in that language, you see the English one. The English title stays saved.
- **New movie and show folders and file names can use the title in this language.** This is off until you switch on **Name new movie and show folders in this language**, in the same card. With it on, the folder shown on a title's page, and the folder and file a download is saved as, use the same title you see there (or the English title when TMDB has no translation). Plex and Jellyfin match by folder name, so check your library after switching it on. A title that already has files keeps the folder those files are in, so changing the language never moves anything. The title a release is searched for on your indexers, and the matching of downloads, stay in English. With **Keep the original file names** on, a file keeps the name it came with.

## What stays in English

- Log lines, and the messages of the server that Mediarium has no translation for (a message that is made up of pieces, or a detail another program sent, such as a provider's own error). Emails and push messages are written in English too. The film or show title and poster in them follow the info language.
- Names of other apps and services (Plex, Jellyfin, TMDB, OpenSubtitles…), release names and quality names such as 1080p. New movie and show folders and file names stay in English unless you switch on the option described above.
- Button and menu names of other apps' own websites in setup steps (for example where to find a key on TMDB), so they match what you see there.
- Music and book details: they come from MusicBrainz and Open Library as those services provide them. (Movie and show details can be read in your language: see below.)

## Helping with a translation

The text lives in `web/src/locales/`, one file per language: `es.json`, `de.json`, `fr.json`, `it.json`, `nl.json`, `pl.json`, `pt.json` (Brazil), `pt-PT.json` (Portugal), `ru.json` and `uk.json`. Each line pairs the English phrase with its translation:

```json
"Add to library": "Añadir a la biblioteca",
"{n} movies": "{n} películas"
```

- Keep the words in braces (`{n}`, `{title}`) exactly as they are. They're filled in when the text is shown. `node tools/i18n-check.mjs` also tells you when a translation has different braces than the English.
- Polish, Russian and Ukrainian have three forms for a count: 1 (`"{n} movie"`), 2 to 4 and 5 or more. The first and the last are the two phrases the code gives (`"{n} movie"`, `"{n} movies"`); the form for 2 to 4 is an extra line with `|few` after the plural phrase, for example `"{n} movies|few": "{n} фильма"`.
- The sentences the server sends (`"That quality is not allowed."`) are in the same files. Where the server puts a name or a number in, the phrase has `{0}`, `{1}`... Keep each number; you may move it to where your language wants it. `go run ./tools/msgextract` rewrites the list of these sentences (`web/src/serverText.json`) from the code.
- A key like `"noun|Search"` is the same English word used in a different place (here the page title, not the button), so it can be translated differently.
- A phrase that's missing shows in English, so nothing ever goes blank.
- `npm run i18n` (in `web/`) lists phrases a language doesn't have yet; `node tools/i18n-check.mjs --strict` fails when any are missing.

In the code, text goes through `t('...')`, `tx('...')` (a sentence with a link or bold word inside), `plural(n, '{n} file', '{n} files')` or `tc('context', '...')`, always with the English written out in full, never built from pieces.

Corrections and new languages are welcome: open an issue or a pull request on GitHub.
