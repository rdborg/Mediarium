// "What's new" after an update: a few highlights per version, shown once in
// each browser after Mediarium moves to a newer version. Newest first. Keep
// each list short and in plain words; the full list is the changelog.
import { t } from './i18n.ts'

export interface ReleaseNotes {
  version: string // "3.0.0"
  items: string[]
}

export const WHATS_NEW: ReleaseNotes[] = [
  {
    version: '3.9.1',
    items: [
      t('UDP trackers and DHT can go through the built-in VPN. The switch is under Settings > Downloading > VPN and proxy, off until you turn it on, and tracker names are looked up through the VPN too.'),
    ],
  },
  {
    version: '3.9.0',
    items: [
      t("New movie and show folders can use the translated title, like Der Pate (1972). It's off until you switch it on under Settings, in the card for the language of movie and show info."),
    ],
  },
  {
    version: '3.7.0',
    items: [
      t("The calendar shows each episode on the day and hour it airs in your own time zone, using air times from TVmaze."),
    ],
  },
  {
    version: '3.6.0',
    items: [
      t("Simkl sign-in works again with the apps Simkl creates now, with a code you enter on Simkl's site."),
      t("Cancel a running download and grab another release in one step, and the statistics stay when you clear finished downloads."),
    ],
  },
  {
    version: '3.5.0',
    items: [
      t("Shows that have ended now show as ended, and the show page says what is being monitored and how much space it uses."),
      t("Connecting Simkl works again, and you can be told when the TRaSH Guides sync changed something."),
    ],
  },
  {
    version: '3.4.0',
    items: [
      t("Home Assistant and other assistants can now sign in to MCP with a login and approval, no key to paste."),
      t("Discover can filter by original language, rating and runtime, and import by hand works from any folder."),
      t("Discord messages show the custom format score, and you can be told when a download client goes down."),
    ],
  },
  {
    version: '3.3.0',
    items: [
      t("Connect an AI assistant or a voice assistant to your library with MCP (off until you switch it on), and send Usenet downloads to SABnzbd or NZBGet."),
      t("See what is inside your video files (codec, HDR, audio and subtitle languages) and filter the library by it."),
      t("Put qualities in your own order and in groups, rescan a music album's folder, and delete a single album file."),
    ],
  },
  {
    version: '3.2.11',
    items: [
      t("A small arrow after a release's score opens why it scored that way and why an automatic search may skip it."),
      t("Sites that run on your own network, like BitMagnet, take your own address when you add or edit them."),
    ],
  },
  {
    version: '3.2.9',
    items: [
      t("Set a minimum number of seeders on each torrent indexer, so releases that would stall are skipped."),
      t("The indexer cards in a row are the same height, with the test result and limits in short lines and the buttons at the bottom."),
    ],
  },
  {
    version: '3.2.8',
    items: [
      t("Custom formats from the TRaSH Guides come in with TRaSH's score in your default profile, and you can add several formats to several profiles at once."),
      t("Keep TRaSH formats up to date with one button or a daily switch, and see the best release first in a manual search."),
    ],
  },
  {
    version: '3.2.7',
    items: [
      t("SubDL subtitles are no longer dropped for a download link of a shape Mediarium did not know, and the Details line shows what the link looked like."),
    ],
  },
  {
    version: '3.2.6',
    items: [
      t("The Indexers & Search page is one full-width card per section, with your indexers in up to three columns."),
      t("When a subtitle search finds nothing, a Details line shows what each site was asked and what came back."),
    ],
  },
  {
    version: '3.2.5',
    items: [
      t("Pick the country for a movie's release dates under Settings > Info, lists and subtitles."),
      t("The Indexers & Search page no longer slides one card over another when you scroll."),
    ],
  },
  {
    version: '3.2.4',
    items: [
      t("An indexer's seeding rules now reach your torrent client, and the Category box stays empty when you clear it."),
      t("Stopping or removing a download also removes the torrent Mediarium sent to qBittorrent or Transmission (you can switch this off)."),
    ],
  },
  {
    version: '3.2.3',
    items: [
      t("A movie's page shows when it is in cinemas and when it comes out digitally, and the calendar marks both days."),
      t("A release that may not replace your file is no longer imported next to it under another name, which stops second copies."),
      t("Subtitle search tries each site by TMDB id, IMDb id and title, and shows an error instead of saying there are none."),
    ],
  },
  {
    version: '3.2.2',
    items: [
      t("The indexer list is sorted by priority, in two columns, with a filter box when you have many."),
      t("The problem report form tells you when a newer version is out, so you can update first."),
      t("Members no longer see server folder paths, and more checks keep a disconnected library drive from being written into."),
    ],
  },
  {
    version: '3.2.1',
    items: [
      t("Indexers have a priority from 1 to 50, like Prowlarr, and Knaben is a built-in site."),
      t("Import your Simkl lists, next to Trakt (new, not yet tried with a real account)."),
      t("Anime numbered by TheTVDB seasons, like One Piece and Jujutsu Kaisen, is placed on the right episodes."),
      t("The e-reader catalogue loads much faster on big libraries, and an unmounted library folder is never written into."),
    ],
  },
  {
    version: '3.2.0',
    items: [
      t("Click an actor, director or writer on a movie or show page to see their photo, life and other work, and add it from there."),
      t("A big unpack is much faster on network shares and Unraid, and Activity shows how far it is."),
      t("Set a temporary download folder on a fast disk, and pick a category in your torrent client for each kind of download."),
      t("Shows you already have can be set to only watch episodes that haven't aired yet, and Rescan finds episodes you added by hand."),
    ],
  },
  {
    version: '3.1.3',
    items: [
      t('Titles with accents or in other alphabets, such as Pokémon, Amélie or Japanese titles, are now found and matched.'),
      t('Big libraries load much faster, and password-protected RAR downloads now open with their password.'),
      t('Keep the original file names if you like, locate a renamed file, change a show\'s folder, set your own book cover and sign out your other devices.'),
      t('Safer by default: the setup code can\'t be skipped, sign-in limits hold, and a site entry can\'t be pointed at your home network.'),
    ],
  },
  {
    version: '3.1.2',
    items: [
      t('Mediarium now learns the daily request limit of an indexer, stops before it is used up, and shows the requests used on the indexer\'s card.'),
      t('The connection check of an indexer no longer uses up the daily requests of free indexers.'),
    ],
  },
  {
    version: '3.1.1',
    items: [
      t('More subtitle and audio languages, searching under other titles, and a switch to take only the languages you list.'),
      t('Movies and shows: see the folder where their files go, point a title at a renamed file or folder, and rescan a movie that has no file. Books: choose a cover by hand.'),
      t('Anime numbered by TheTVDB seasons, such as One Piece, now imports, and Library has its own entries in the left menu.'),
    ],
  },
  {
    version: '3.1.0',
    items: [
      t('Five more screen languages: Italian, Dutch, Polish, Russian and Ukrainian. Error messages and notes from the server now come in your language too.'),
      t('Comics: .cbz, .cbr and .cb7 files open in the reader, one page or two, left to right or right to left.'),
      t('Plan your downloads: a speed schedule by day and hour, a weekly summary message, a Quiet for a while button and a switch to start newer releases first.'),
      t('The settings file now holds your indexers, Usenet servers, media servers and quality profiles too.'),
      t('A Backdrops view in the calendar, a page-turn animation in the ebook reader, and an Add-ons page for the programs you run next to Mediarium.'),
      t('A better installed app: shortcuts on the icon, a page that says when the server cannot be reached, and layouts for tablets and foldables.'),
      t('Send indexer traffic through the VPN, pick a book by hand when it is not found, and replace a movie with a smaller version.'),
      t('Movies and shows can have their own default quality profile, and each season has its own Find missing button.'),
    ],
  },
  {
    version: '3.0.4',
    items: [
      t('Transmission works as a download client (Settings > Downloading > Torrents).'),
      t('Keep your settings in a YAML file and apply it with the app config command (see the docs page Keep your settings in a file).'),
      t('In Activity, send a waiting download to the front, or tick several downloads and pause, resume, stop or move them together.'),
      t('Shows say if they have ended, episodes show their file size, release lists have an Age column, and the Library sorts without a leading The.'),
      t('Apps like Tdarr that change a file can now tell Mediarium to look again, and your media servers are asked to scan the folder.'),
    ],
  },
  {
    version: '3.0.0',
    items: [
      t('A new look: a darker design with a colour and a shape for each kind of media, floating shapes behind the pages, and a layout that works on a phone.'),
      t('A music player built in: play an album or a whole artist, and it keeps going as you browse. It can open in a window of its own.'),
      t('Pick several audio languages in the order you like, including the language each film or show was made in (Settings > Library > Quality).'),
      t('The trailer plays behind a movie or show page, in place of the still picture. You can switch it off in Settings.'),
      t('Find the right number of Usenet connections for your provider with one button (Settings > Downloading > Usenet).'),
      t('Titles, plots and posters in about thirty languages, chosen apart from the screen language (Settings > Info, lists and subtitles).'),
      t('Send torrents to qBittorrent, RDT-Client or Decypharr, or to Real-Debrid or TorBox (Settings > Downloading > Torrents).'),
      t('Subtitle add-ons: paste the address of a Stremio add-on to search sites like subs.ro too (Settings > Info, lists and subtitles > Subtitles).'),
      t('Silo as a media server, with Watch in Silo buttons on every title.'),
      t('Skip bad releases is on in every profile: whole discs, 3D, upscaled copies and burned-in subtitles are left out.'),
      t('A calendar of what is coming out, and a clearer Activity page for what is downloading, waiting or failed.'),
      t('Keep a movie or show in two qualities, such as 4K next to 1080p (Settings > Library > Folders).'),
      t('Custom formats, with ready-made formats and profiles from the TRaSH Guides in a couple of clicks (Settings > Library > Quality).'),
      t('Two-step sign-in with a code from an authenticator app, offered when Mediarium can be reached from outside your home.'),
      t('Push notifications to your phone or computer, with no app needed (Settings > Connections > Notifications).'),
    ],
  },
]

// titleVersion is the version the dialog names: the one running, without a
// pre-release tag or a trailing .0 ("3.1.3-beta.2" -> "3.1.3", "3.1.0" -> "3.1").
export function titleVersion(current: string): string {
  const p = parts(current)
  return p ? p.join('.').replace(/\.0$/, '') : current
}

export const SEEN_KEY = 'mediarium-seen-version'

// parts reads "3.0.0-beta.2" as [3, 0, 0]; a pre-release counts as its
// version, so betas of 3.0.0 show the 3.0.0 notes.
function parts(v: string): number[] | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(v.trim())
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null
}

export function compareVersions(a: string, b: string): number {
  const x = parts(a)
  const y = parts(b)
  if (!x || !y) return 0
  for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] - y[i]
  return 0
}

// notesToShow lists the notes for every version newer than lastSeen, up to
// and including current. Nothing on a browser that has never seen
// Mediarium before (lastSeen null), or for a version that isn't a number.
export function notesToShow(lastSeen: string | null, current: string, notes: ReleaseNotes[] = WHATS_NEW): ReleaseNotes[] {
  if (!lastSeen || !parts(current) || !parts(lastSeen)) return []
  return notes.filter((n) => compareVersions(n.version, lastSeen) > 0 && compareVersions(n.version, current) <= 0)
}
