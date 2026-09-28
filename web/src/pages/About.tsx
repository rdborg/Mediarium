import { useEffect, useState } from 'react'
import tmdbLogo from '../assets/tmdb-logo.svg'
import { api } from '../api'
import BrandMark from '../components/BrandMark'
import Icon, { type IconName } from '../components/Icon'
import { DOCS_URL, REPO_URL } from '../docs'

const FEATURES: { icon: IconName; title: string; text: string; color: string }[] = [
  { icon: 'search', title: 'Finds releases', text: 'Searches your indexers, ranks results against your quality profile and picks the best one. Like Radarr, Sonarr and Prowlarr in one place.', color: 'var(--c-discover)' },
  { icon: 'download', title: 'Downloads them itself', text: 'A built-in Usenet downloader (multi-server, PAR2 repair, unpacking) and a built-in torrent client. Nothing else to install or connect.', color: 'var(--c-activity)' },
  { icon: 'folder', title: 'Files it neatly', text: 'Moves finished downloads into your library with the naming you choose. Hardlinks when it can, so no extra disk space. It never overwrites or deletes your files on its own.', color: 'var(--c-library)' },
  { icon: 'chat', title: 'Gets subtitles', text: 'Fetches subtitles in your languages from OpenSubtitles and keeps looking for the ones that are missing.', color: 'var(--c-calendar)' },
  { icon: 'calendar', title: 'Keeps watch', text: 'Watches release dates and air dates, hunts for missing episodes and better quality, and tells you when something needs attention.', color: 'var(--c-wanted)' },
  { icon: 'shield', title: 'Stays private', text: 'Optional built-in WireGuard VPN for torrent traffic with a kill switch, and it needs no special container permissions. Runs entirely on your own machine.', color: 'var(--c-dashboard)' },
]

const CREDITS: { name: string; what: string; href: string }[] = [
  { name: 'OpenSubtitles', what: 'Subtitles', href: 'https://www.opensubtitles.com/' },
  { name: 'Trakt', what: 'Public list import', href: 'https://trakt.tv/' },
  { name: 'Sora and IBM Plex', what: 'Fonts (SIL Open Font License)', href: 'https://fonts.google.com/' },
  { name: 'nwaples/rardecode', what: 'Unpacking RAR archives (BSD-2-Clause)', href: 'https://github.com/nwaples/rardecode' },
  { name: '7-Zip', what: 'Unpacking 7z archives', href: 'https://www.7-zip.org/' },
  { name: 'par2cmdline', what: 'Repairing incomplete downloads', href: 'https://github.com/Parchive/par2cmdline' },
  { name: 'anacrolix/torrent', what: 'The BitTorrent engine (MPL-2.0)', href: 'https://github.com/anacrolix/torrent' },
  { name: 'wireguard-go', what: 'The userspace VPN tunnel (MIT)', href: 'https://www.wireguard.com/' },
  { name: 'Radarr, Sonarr, Prowlarr, SABnzbd, Bazarr', what: 'The projects that showed how this should work', href: 'https://wiki.servarr.com/' },
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
          <p>One app that finds, downloads, organises and keeps your movies and shows up to date. Self-hosted, open source, and it runs as a single container.</p>
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

      <div className="group-cols" style={{ marginTop: 22 }}>
        <fieldset className="group folders">
          <legend>
            <Icon name="heart" size={14} /> Credits
          </legend>
          <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 12 }}>
            <a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer">
              <img src={tmdbLogo} alt="The Movie Database (TMDB)" style={{ height: 24 }} />
            </a>
          </div>
          <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>This product uses the TMDB API but is not endorsed or certified by TMDB. All movie and show information and artwork come from TMDB.</p>
          <ul className="credit-list">
            {CREDITS.map((c) => (
              <li key={c.name}>
                <a href={c.href} target="_blank" rel="noreferrer">
                  {c.name}
                </a>
                <span>{c.what}</span>
              </li>
            ))}
          </ul>
        </fieldset>

        <fieldset className="group alerts">
          <legend>
            <Icon name="shield" size={14} /> Legal and responsible use
          </legend>
          <p style={{ marginTop: 0 }}>
            Mediarium is a general-purpose automation and organising tool, provided for educational and personal use. It does not host, index, link to or supply any content, and it is not affiliated with TMDB, Trakt, OpenSubtitles or any indexer, Usenet or torrent provider.
          </p>
          <p>
            You are solely responsible for what you download and for following the laws of your country and the terms of the services you use. It is intended for material you have the legal right to obtain: your own backups, public-domain and freely licensed works, and content you are licensed to use.
          </p>
          <p style={{ color: 'var(--text-dim)', marginBottom: 0 }}>
            Provided as is, with no warranty of any kind, under the AGPL-3.0. Trademarks belong to their owners. This is a plain-language notice, not legal advice.
          </p>
        </fieldset>
      </div>
    </div>
  )
}
