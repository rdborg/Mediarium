import { useEffect, useState } from 'react'
import tmdbLogo from '../assets/tmdb-logo.svg'
import { api } from '../api'
import BrandMark from '../components/BrandMark'
import Icon, { type IconName } from '../components/Icon'
import { DOCS_URL, REPO_URL } from '../docs'
import { helpers } from '../community'
import { t, tx } from '../i18n'

const FEATURES: { icon: IconName; title: string; text: string; color: string }[] = [
  { icon: 'search', title: t('Finds releases'), text: t('Searches your indexers for movies, shows, music, ebooks and audiobooks, and picks the best release for your quality profile.'), color: 'var(--c-discover)' },
  { icon: 'download', title: t('Downloads them itself'), text: t('A built-in Usenet downloader and torrent client. Prefer your own? Use qBittorrent, Real-Debrid or TorBox.'), color: 'var(--c-activity)' },
  { icon: 'folder', title: t('Files it neatly'), text: t('Moves finished downloads into your library with the naming you choose, using hardlinks when it can. It never overwrites or deletes your files by itself.'), color: 'var(--c-library)' },
  { icon: 'chat', title: t('Gets subtitles'), text: t('Finds subtitles in your languages from OpenSubtitles, SubDL and subtitle add-ons, and keeps looking for the missing ones.'), color: 'var(--c-rose)' },
  { icon: 'calendar', title: t('Keeps watch'), text: t('Tracks release dates and looks again for what is missing. People you share with can ask for titles, and you hear when something needs you.'), color: 'var(--c-wanted)' },
  { icon: 'shield', title: t('Stays private'), text: t('An optional built-in WireGuard VPN with a kill switch, and no special container permissions. Everything runs on your own machine.'), color: 'var(--c-dashboard)' },
]

interface Credit {
  name: string
  href: string
  used: string
  desc: string
  licence?: string
}

interface CreditGroup {
  group: string
  items: Credit[]
}

// Only things Mediarium really uses: services it talks to, code it is built
// with or runs, and the fonts and icons you see. One card per group; a cell
// holds one card, or a few small ones stacked so a row stays level.
const CREDIT_CELLS: { wide?: boolean; groups: CreditGroup[] }[] = [
  {
    groups: [
      {
        group: t('Movies, shows and lists'),
        items: [
          { name: 'The Movie Database (TMDB)', href: 'https://www.themoviedb.org/', used: t('Movie and show information'), desc: t('Titles, posters, summaries, ratings, trailers, cast and episode lists. This product uses the TMDB API but is not endorsed or certified by TMDB.') },
          { name: 'Trakt', href: 'https://trakt.tv/', used: t('List import'), desc: t('Reading public Trakt lists so you can add everything on a list at once.') },
          { name: 'Simkl', href: 'https://simkl.com/', used: t('List import'), desc: t('Reading your own Simkl lists, once you connect your Simkl account.') },
          { name: 'MDBList', href: 'https://mdblist.com/', used: t('List import'), desc: t('Reading your MDBList lists, with your own MDBList key.') },
          { name: 'TheXEM', href: 'https://thexem.info/', used: t('Scene numbering'), desc: t('Which episode a release means when it counts the show differently from TMDB.') },
          { name: 'OMDb', href: 'https://www.omdbapi.com/', used: t('IMDb, Rotten Tomatoes and Metacritic scores'), desc: t('The scores on film and show pages and Discover cards, only when you add your own OMDb key.') },
          { name: 'TheTVDB', href: 'https://thetvdb.com/', used: t('Missing seasons and episodes'), desc: t('Seasons and episodes a show is missing on TMDB, only when you add your own TheTVDB key. Metadata provided by TheTVDB.') },
          { name: 'TVmaze', href: 'https://www.tvmaze.com/', used: t('Air times'), desc: t('The time each episode airs, so the calendar can show it in your time zone. Data from TVmaze, licensed CC BY-SA 4.0.') },
          { name: 'AniList', href: 'https://anilist.co/', used: t('Anime names'), desc: t('The other names of an anime, used to find releases.') },
          { name: 'Fribb/anime-lists', href: 'https://github.com/Fribb/anime-lists', used: t('Anime numbering'), desc: t('Which TMDB season and episode each anime entry matches, built from the Anime-Lists project.') },
        ],
      },
    ],
  },
  {
    groups: [
      {
        group: t('Music and books'),
        items: [
          { name: 'MusicBrainz', href: 'https://musicbrainz.org/', used: t('Music information'), desc: t('Artists, albums, releases and track lists, from the open music encyclopedia.') },
          { name: 'Cover Art Archive', href: 'https://coverartarchive.org/', used: t('Album covers'), desc: t('The front cover of each album, saved once on your server.') },
          { name: 'ListenBrainz', href: 'https://listenbrainz.org/', used: t('Popular music'), desc: t('What people are listening to and what is newly released, shown on Discover.') },
          { name: 'LRCLIB', href: 'https://lrclib.net/', used: t('Lyrics'), desc: t('Lyrics that follow the song, for tracks whose files have none.') },
          { name: 'Open Library', href: 'https://openlibrary.org/', used: t('Book information'), desc: t('Book details, authors, series and covers for ebooks and audiobooks, from the Internet Archive.') },
          { name: 'Audnexus', href: 'https://audnex.us/', used: t('Audiobook details'), desc: t('Who reads an audiobook and how long it runs, from Audible’s catalogue. Audible’s public search finds the book first.') },
          { name: 'Hardcover', href: 'https://hardcover.app/', used: t('Book series (optional)'), desc: t('More book series and release dates, only when you add your own Hardcover token.') },
          { name: 'Google Books', href: 'https://books.google.com/', used: t('Book search (optional)'), desc: t('A missing cover or description, and the details of a book only Google Books knows. No API key is required.') },
        ],
      },
    ],
  },
  {
    groups: [
      {
        group: t('Subtitles'),
        items: [
          { name: 'OpenSubtitles', href: 'https://www.opensubtitles.com/', used: t('Subtitles'), desc: t('Searching for and downloading subtitles in your languages.') },
          { name: 'SubDL', href: 'https://subdl.com/', used: t('Subtitles (optional)'), desc: t('A second place to find subtitles, only when you add your own SubDL key.') },
        ],
      },
      {
        group: t('Indexers and optional helpers'),
        items: [
          { name: 'Prowlarr/Indexers', href: 'https://github.com/Prowlarr/Indexers', used: t('Torrent site definitions'), desc: t('The community-maintained descriptions of how to search each site, downloaded to your server when you open the site list.') },
          { name: 'Knaben', href: 'https://knaben.org/', used: t('Torrent site (optional)'), desc: t('A public torrent search engine, asked only when you add it from the site list.') },
          { name: 'FlareSolverr', href: 'https://github.com/FlareSolverr/FlareSolverr', used: t('Optional helper'), desc: t('Runs next to Mediarium if you add it, to get past Cloudflare checks on some sites.'), licence: 'MIT' },
          { name: 'Real-Debrid, TorBox and Premiumize', href: 'https://real-debrid.com/', used: t('Debrid services (optional)'), desc: t('Download torrents on their own servers, so none of that traffic leaves your home. Only when you connect one.') },
          { name: 'Plex', href: 'https://www.plex.tv/', used: t('Sign-in and watchlist (optional)'), desc: t('Sign in with Plex and your Plex watchlist, only when you use them.') },
        ],
      },
    ],
  },
  {
    wide: true,
    groups: [
      {
        group: t('Libraries and software'),
        items: [
          { name: 'anacrolix/torrent', href: 'https://github.com/anacrolix/torrent', used: t('Torrent client'), desc: t('The BitTorrent engine inside Mediarium: peers, DHT, magnet links and seeding.'), licence: 'MPL-2.0' },
          { name: 'wireguard-go and gVisor', href: 'https://www.wireguard.com/', used: t('Built-in VPN'), desc: t('A WireGuard tunnel that runs inside the app, with no special container permissions.'), licence: 'MIT, Apache-2.0' },
          { name: 'rardecode', href: 'https://github.com/nwaples/rardecode', used: t('Unpacking'), desc: t('Opens RAR archives, including multi-part ones, from Usenet downloads.'), licence: 'BSD-2-Clause' },
          { name: 'dhowden/tag', href: 'https://github.com/dhowden/tag', used: t('Music tags'), desc: t('Reads artist, album and track information from music files.'), licence: 'BSD-2-Clause' },
          { name: 'SQLite (modernc.org/sqlite)', href: 'https://gitlab.com/cznic/sqlite', used: t('Database'), desc: t('Stores your library, settings and history in one file, without any extra software.'), licence: 'BSD-3-Clause' },
          { name: 'Go x/crypto and x/net', href: 'https://pkg.go.dev/golang.org/x/crypto', used: t('Security and web pages'), desc: t('Password hashing, VPN keys, and reading torrent site pages.'), licence: 'BSD-3-Clause' },
          { name: 'go-yaml', href: 'https://github.com/go-yaml/yaml', used: t('Site definitions'), desc: t('Reads the torrent site definition files.'), licence: 'MIT, Apache-2.0' },
          { name: 'go-webauthn', href: 'https://github.com/go-webauthn/webauthn', used: t('Passkeys'), desc: t('Signing in with a passkey.'), licence: 'BSD-3-Clause' },
          { name: 'go-oidc', href: 'https://github.com/coreos/go-oidc', used: t('Single sign-on'), desc: t('Signing in through an OpenID Connect provider.'), licence: 'Apache-2.0' },
          { name: 'go-ldap', href: 'https://github.com/go-ldap/ldap', used: t('Directory sign-in'), desc: t('Signing in with an LDAP or Active Directory account.'), licence: 'MIT' },
          { name: 'kepubify', href: 'https://github.com/pgaskin/kepubify', used: t('Kobo books'), desc: t('Turns EPUB books into the format Kobo e-readers read.'), licence: 'MIT' },
          { name: 'epub.js', href: 'https://github.com/futurepress/epub.js', used: t('Ebook reader'), desc: t('Draws EPUB books page by page in Mediarium Books.'), licence: 'BSD-2-Clause' },
          { name: 'React and React Router', href: 'https://react.dev/', used: t('Web interface'), desc: t("The screens you're looking at."), licence: 'MIT' },
        ],
      },
    ],
  },
  {
    groups: [
      {
        group: t('Tools in the Docker image'),
        items: [
          { name: '7-Zip', href: 'https://www.7-zip.org/', used: t('Unpacking'), desc: t('Opens 7z archives. Included in the Docker image.'), licence: 'LGPL' },
          { name: 'par2cmdline', href: 'https://github.com/Parchive/par2cmdline', used: t('Repair'), desc: t('Checks and repairs Usenet downloads that arrive with missing pieces. Included in the Docker image.'), licence: 'GPL-2.0' },
        ],
      },
      {
        group: t('Fonts and icons'),
        items: [
          { name: 'Wix Madefor, Host Grotesk and Reddit Mono', href: 'https://fonts.google.com/', used: t('Fonts'), desc: t('Headings in Wix Madefor Display, text in Host Grotesk, numbers and codes in Reddit Mono.'), licence: 'SIL Open Font License' },
          { name: 'Simple Icons', href: 'https://simpleicons.org/', used: t('Service logos'), desc: t('The Discord, Telegram, Slack and ntfy marks on the Notifications page. Logos belong to their owners.'), licence: 'CC0-1.0' },
        ],
      },
    ],
  },
]

// What Mediarium is, where it comes from, and the credits and legal wording
// that the services it uses ask for.
export default function About() {
  const [version, setVersion] = useState('')

  useEffect(() => {
    api.version().then((v) => setVersion(v.version)).catch(() => setVersion(''))
  }, [])

  return (
    <div className="about">
      <section className="hero-welcome about-hero">
        <div>
          <h1 style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
            <BrandMark className="brand-mark" />
            <span>Mediarium</span>
          </h1>
          <p>{t('One app that finds, downloads and organises your movies, shows, music, ebooks and audiobooks, and keeps them up to date. Self-hosted, open source, and it runs as a single container.')}</p>
        </div>
        <div className="hero-pills">
          <span className="hero-pill" style={{ ['--pc' as string]: 'var(--c-dashboard)' }}>
            <Icon name="info" size={15} /> {t('Version {version}', { version: version || 'dev' })}
          </span>
          <a className="hero-pill" href={REPO_URL} target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-discover)' }}>
            <Icon name="external" size={15} /> {t('Source on GitHub')}
          </a>
          <a className="hero-pill" href={DOCS_URL} target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-activity)' }}>
            <Icon name="info" size={15} /> {t('Documentation')}
          </a>
          <a className="hero-pill" href="https://www.gnu.org/licenses/agpl-3.0.html" target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-library)' }}>
            <Icon name="shield" size={15} /> {t('AGPL-3.0 licence')}
          </a>
          <a className="hero-pill" href={REPO_URL + '/blob/main/THIRD_PARTY_NOTICES.md'} target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-settings)' }}>
            <Icon name="list" size={15} /> {t('Third-party licences')}
          </a>
        </div>
      </section>

      <div className="feature-wrap">
        <div className="feature-grid">
          {FEATURES.map((f) => (
            <section key={f.title} className="tile" style={{ ['--tc' as string]: f.color }}>
              <div className="tile-head">
                <span className="tile-ico">
                  <Icon name={f.icon} size={18} />
                </span>
                <h2>{f.title}</h2>
              </div>
              <p style={{ margin: 0, color: 'var(--text-dim)' }}>{f.text}</p>
            </section>
          ))}
        </div>
      </div>

      <section className="legal-card" style={{ marginTop: 22 }} aria-labelledby="legal-title">
        <div className="legal-head">
          <span className="legal-ico">
            <Icon name="shield" size={30} />
          </span>
          <div>
            <h2 id="legal-title">{t('Legal and responsible use')}</h2>
            <p>{t("Read this first. It's short, and it matters.")}</p>
          </div>
        </div>
        <div className="legal-cols">
          <div className="legal-col">
            <h3>{t('What Mediarium is')}</h3>
            <p>
              {t('Mediarium is a general-purpose automation and organising tool, provided for educational and personal use. It does not host, index, link to or supply any content, and it is not affiliated with TMDB, Trakt, OpenSubtitles or any indexer, Usenet or torrent provider.')}
            </p>
          </div>
          <div className="legal-col">
            <h3>{t('What you are responsible for')}</h3>
            <p>
              {t('You are solely responsible for what you download and for following the laws of your country and the terms of the services you use. It is intended for material you have the legal right to obtain: your own backups, public-domain and freely licensed works, and content you are licensed to use.')}
            </p>
          </div>
          <div className="legal-col">
            <h3>{t('No warranty')}</h3>
            <p>
              {t("Provided as is, with no warranty of any kind, under the AGPL-3.0. Trademarks belong to their owners. It's a plain-language notice, not legal advice.")}
            </p>
          </div>
        </div>
        <p className="legal-foot">
          <Icon name="info" size={15} />
          <span>
            {tx('Read the full text in the {notice}.', {
              notice: (
                <a href={`${DOCS_URL}/LEGAL.md`} target="_blank" rel="noreferrer">
                  {t('legal notice')}
                </a>
              ),
            })}
          </span>
        </p>
      </section>

      <div className="about-stack credit-area" style={{ marginTop: 22 }}>
        <fieldset className="group alerts">
          <legend>
            <Icon name="sparkles" size={14} /> {t('Thank you: people whose ideas shaped Mediarium')}
          </legend>
          <p className="field-help" style={{ marginTop: 0 }}>
            {t('Every idea and bug report here came from someone who uses Mediarium. Have one? Send it on GitHub Discussions or Reddit.')}
          </p>
          <ul className="helper-grid">
            {helpers().map((h) => (
              <li key={h.name} className="helper-card">
                <div className="credit-top">
                  <a href={h.href} target="_blank" rel="noreferrer">
                    {h.where === 'Reddit' ? 'u/' : '@'}
                    {h.name}
                  </a>
                  <span className="badge">{h.where}</span>
                </div>
                <ul>
                  {h.ideas.map((idea) => (
                    <li key={idea}>{idea}</li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        </fieldset>

        <fieldset className="group folders">
          <legend>
            <Icon name="heart" size={14} /> {t('Credits: what Mediarium is built with')}
          </legend>
          <div className="credit-grid">
            {CREDIT_CELLS.map((cell, i) => (
              <div key={i} className={`credit-cell${cell.wide ? ' wide' : ''}${cell.groups.length > 1 ? ' stack' : ''}`}>
                {cell.groups.map((g) => (
                  <section key={g.group} className="credit-card">
                    <h3>{g.group}</h3>
                    <ul className="credit-list">
                      {g.items.map((c) => (
                        <li key={c.name}>
                          <div className="credit-top">
                            <a href={c.href} target="_blank" rel="noreferrer">
                              {c.name === 'The Movie Database (TMDB)' && <img src={tmdbLogo} alt="" className="credit-logo" />}
                              {c.name}
                            </a>
                            <span className="badge">{c.used}</span>
                            {c.licence && <small className="credit-licence">{c.licence}</small>}
                          </div>
                          <p>{c.desc}</p>
                        </li>
                      ))}
                    </ul>
                  </section>
                ))}
              </div>
            ))}
          </div>
        </fieldset>
      </div>
    </div>
  )
}
