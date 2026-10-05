import { useEffect, useState } from 'react'
import tmdbLogo from '../assets/tmdb-logo.svg'
import { api } from '../api'
import BrandMark from '../components/BrandMark'
import Icon, { type IconName } from '../components/Icon'
import { DOCS_URL, REPO_URL } from '../docs'

const FEATURES: { icon: IconName; title: string; text: string; color: string }[] = [
  { icon: 'search', title: 'Finds releases', text: 'Searches your indexers, ranks every result against your quality profile and picks the best one.', color: 'var(--c-discover)' },
  { icon: 'download', title: 'Downloads them itself', text: 'A built-in Usenet downloader (multi-server, PAR2 repair, unpacking) and a built-in torrent client. Nothing else to install or connect.', color: 'var(--c-activity)' },
  { icon: 'folder', title: 'Files it neatly', text: 'Moves finished downloads into your library with the naming you choose. It uses hardlinks when it can, so it takes no extra disk space, and it never overwrites or deletes your files on its own.', color: 'var(--c-library)' },
  { icon: 'chat', title: 'Gets subtitles', text: 'Fetches subtitles in your languages from OpenSubtitles and keeps looking for the ones that are missing.', color: 'var(--c-rose)' },
  { icon: 'calendar', title: 'Keeps watch', text: 'Tracks release and air dates, keeps looking for missing episodes, and for better quality when you switch that on, and tells you when something needs attention.', color: 'var(--c-wanted)' },
  { icon: 'shield', title: 'Stays private', text: 'An optional built-in WireGuard VPN for torrent traffic, with a kill switch and no special container permissions. Everything runs on your own machine.', color: 'var(--c-dashboard)' },
]

interface Credit {
  name: string
  href: string
  used: string
  desc: string
  licence?: string
}

// Only things Mediarium really uses: services it talks to, code it is built
// with or runs, and the fonts and icons you see.
const CREDITS: { group: string; items: Credit[] }[] = [
  {
    group: 'Services',
    items: [
      { name: 'The Movie Database (TMDB)', href: 'https://www.themoviedb.org/', used: 'Movie and show information', desc: 'Titles, posters, summaries, ratings, trailers, cast and episode lists. This product uses the TMDB API but is not endorsed or certified by TMDB.' },
      { name: 'MusicBrainz', href: 'https://musicbrainz.org/', used: 'Music information', desc: 'Artists, albums, releases and track lists, from the open music encyclopedia.' },
      { name: 'Cover Art Archive', href: 'https://coverartarchive.org/', used: 'Album covers', desc: 'The front cover of each album, saved once on your server.' },
      { name: 'ListenBrainz', href: 'https://listenbrainz.org/', used: 'Popular music', desc: 'What people are listening to and what is newly released, shown on Discover.' },
      { name: 'Open Library', href: 'https://openlibrary.org/', used: 'Book information', desc: 'Book details, authors and covers for ebooks and audiobooks, from the Internet Archive.' },
      { name: 'OpenSubtitles', href: 'https://www.opensubtitles.com/', used: 'Subtitles', desc: 'Searching for and downloading subtitles in your languages.' },
      { name: 'Trakt', href: 'https://trakt.tv/', used: 'List import', desc: 'Reading public Trakt lists so you can add everything on a list at once.' },
      { name: 'Prowlarr/Indexers', href: 'https://github.com/Prowlarr/Indexers', used: 'Torrent site definitions', desc: 'The community-maintained descriptions of how to search each site, downloaded to your server when you open the site list.' },
    ],
  },
  {
    group: 'Software',
    items: [
      { name: 'anacrolix/torrent', href: 'https://github.com/anacrolix/torrent', used: 'Torrent client', desc: 'The BitTorrent engine inside Mediarium: peers, DHT, magnet links and seeding.', licence: 'MPL-2.0' },
      { name: 'wireguard-go and gVisor', href: 'https://www.wireguard.com/', used: 'Built-in VPN', desc: 'A WireGuard tunnel that runs inside the app, with no special container permissions.', licence: 'MIT, Apache-2.0' },
      { name: 'rardecode', href: 'https://github.com/nwaples/rardecode', used: 'Unpacking', desc: 'Opens RAR archives, including multi-part ones, from Usenet downloads.', licence: 'BSD-2-Clause' },
      { name: 'dhowden/tag', href: 'https://github.com/dhowden/tag', used: 'Music tags', desc: 'Reads artist, album and track information from music files.', licence: 'BSD-2-Clause' },
      { name: '7-Zip', href: 'https://www.7-zip.org/', used: 'Unpacking', desc: 'Opens 7z archives. Included in the Docker image.', licence: 'LGPL' },
      { name: 'par2cmdline', href: 'https://github.com/Parchive/par2cmdline', used: 'Repair', desc: 'Checks and repairs Usenet downloads that arrive with missing pieces. Included in the Docker image.', licence: 'GPL-2.0' },
      { name: 'SQLite (modernc.org/sqlite)', href: 'https://gitlab.com/cznic/sqlite', used: 'Database', desc: 'Stores your library, settings and history in one file, without any extra software.', licence: 'BSD-3-Clause' },
      { name: 'Go x/crypto and x/net', href: 'https://pkg.go.dev/golang.org/x/crypto', used: 'Security and web pages', desc: 'Password hashing, VPN keys, and reading torrent site pages.', licence: 'BSD-3-Clause' },
      { name: 'epub.js', href: 'https://github.com/futurepress/epub.js', used: 'Ebook reader', desc: 'Draws EPUB books page by page in Mediarium Books.', licence: 'BSD-2-Clause' },
      { name: 'go-yaml', href: 'https://github.com/go-yaml/yaml', used: 'Site definitions', desc: 'Reads the torrent site definition files.', licence: 'MIT, Apache-2.0' },
      { name: 'React and React Router', href: 'https://react.dev/', used: 'Web interface', desc: "The screens you're looking at.", licence: 'MIT' },
      { name: 'FlareSolverr', href: 'https://github.com/FlareSolverr/FlareSolverr', used: 'Optional helper', desc: 'Runs next to Mediarium if you add it, to get past Cloudflare checks on some sites.', licence: 'MIT' },
    ],
  },
  {
    group: 'Fonts and icons',
    items: [
      { name: 'Sora and IBM Plex', href: 'https://fonts.google.com/', used: 'Fonts', desc: 'Headings in Sora, text in IBM Plex Sans and Plex Mono.', licence: 'SIL Open Font License' },
      { name: 'Simple Icons', href: 'https://simpleicons.org/', used: 'Service logos', desc: 'The Discord, Telegram, Slack and ntfy marks on the Notifications page. Logos belong to their owners.', licence: 'CC0-1.0' },
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
          <p>One app that finds, downloads and organises your movies, shows and music, and keeps them up to date. Self-hosted, open source, and it runs as a single container.</p>
        </div>
        <div className="hero-pills">
          <span className="hero-pill" style={{ ['--pc' as string]: 'var(--c-dashboard)' }}>
            <Icon name="info" size={15} /> Version {version || 'dev'}
          </span>
          <a className="hero-pill" href={REPO_URL} target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-discover)' }}>
            <Icon name="external" size={15} /> Source on GitHub
          </a>
          <a className="hero-pill" href={DOCS_URL} target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-activity)' }}>
            <Icon name="info" size={15} /> Documentation
          </a>
          <a className="hero-pill" href="https://www.gnu.org/licenses/agpl-3.0.html" target="_blank" rel="noreferrer" style={{ ['--pc' as string]: 'var(--c-library)' }}>
            <Icon name="shield" size={15} /> AGPL-3.0 licence
          </a>
        </div>
      </section>

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

      <section className="legal-card" style={{ marginTop: 22 }} aria-labelledby="legal-title">
        <div className="legal-head">
          <span className="legal-ico">
            <Icon name="shield" size={30} />
          </span>
          <div>
            <h2 id="legal-title">Legal and responsible use</h2>
            <p>Read this first. It's short, and it matters.</p>
          </div>
        </div>
        <div className="legal-cols">
          <div className="legal-col">
            <h3>What Mediarium is</h3>
            <p>
              Mediarium is a general-purpose automation and organising tool, provided for educational and personal use. It does not host, index, link to or supply any content, and it is not affiliated with TMDB, Trakt, OpenSubtitles or any indexer, Usenet or torrent provider.
            </p>
          </div>
          <div className="legal-col">
            <h3>What you are responsible for</h3>
            <p>
              You are solely responsible for what you download and for following the laws of your country and the terms of the services you use. It is intended for material you have the legal right to obtain: your own backups, public-domain and freely licensed works, and content you are licensed to use.
            </p>
          </div>
          <div className="legal-col">
            <h3>No warranty</h3>
            <p>
              Provided as is, with no warranty of any kind, under the AGPL-3.0. Trademarks belong to their owners. It&apos;s a plain-language notice, not legal advice.
            </p>
          </div>
        </div>
        <p className="legal-foot">
          <Icon name="info" size={15} />
          <span>
            Read the full text in the{' '}
            <a href={`${DOCS_URL}/LEGAL.md`} target="_blank" rel="noreferrer">
              legal notice
            </a>
            .
          </span>
        </p>
      </section>

      <div className="about-stack" style={{ marginTop: 22 }}>
        <fieldset className="group folders">
          <legend>
            <Icon name="heart" size={14} /> Credits: what Mediarium is built with
          </legend>
          <div className="credit-cols">
          {CREDITS.map((g) => (
            <div key={g.group} className="credit-group">
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
            </div>
          ))}
          </div>
        </fieldset>

      </div>
    </div>
  )
}
