import { useState } from 'react'
import type { CastMember, SeasonInfo } from '../api'
import './TitlePreview.css'

// The outside pages worth linking to: the title's own website, IMDb and TMDB.
export function titleLinks(kind: 'movie' | 'tv', tmdbId: number, imdbId?: string, homepage?: string): { label: string; url: string }[] {
  const links: { label: string; url: string }[] = []
  if (homepage && /^https?:\/\//i.test(homepage)) links.push({ label: 'Website', url: homepage })
  if (imdbId && /^tt\d+$/.test(imdbId)) links.push({ label: 'IMDb', url: `https://www.imdb.com/title/${imdbId}/` })
  links.push({ label: 'TMDB', url: `https://www.themoviedb.org/${kind === 'movie' ? 'movie' : 'tv'}/${tmdbId}` })
  return links
}

function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w.charAt(0).toUpperCase())
    .join('')
}

function Person({ p }: { p: CastMember }) {
  const [broken, setBroken] = useState(false)
  return (
    <li className="preview-person">
      <span className="preview-photo">{p.profileUrl && !broken ? <img src={p.profileUrl} alt="" loading="lazy" onError={() => setBroken(true)} /> : initials(p.name)}</span>
      <strong>{p.name}</strong>
      {p.character && <small>{p.character}</small>}
    </li>
  )
}

// Who is in it, in billing order, with their photos.
export function CastCard({ cast }: { cast: CastMember[] }) {
  if (cast.length === 0) return null
  return (
    <section className="card" style={{ marginBottom: 24 }}>
      <h2>Cast</h2>
      <ul className="preview-cast" style={{ listStyle: 'none', padding: 0, margin: 0 }}>
        {cast.map((p) => (
          <Person key={`${p.name}-${p.character ?? ''}`} p={p} />
        ))}
      </ul>
    </section>
  )
}

// The seasons of a show and how many episodes each has.
export function SeasonsCard({ seasons }: { seasons: SeasonInfo[] }) {
  if (seasons.length === 0) return null
  return (
    <section className="card" style={{ marginBottom: 24 }}>
      <h2>Seasons</h2>
      <ul className="preview-seasons">
        {seasons.map((s) => (
          <li key={s.number}>
            <strong>{s.name}</strong>
            <small>
              {s.episodes} {s.episodes === 1 ? 'episode' : 'episodes'}
              {s.airDate ? ` · ${s.airDate.slice(0, 4)}` : ''}
            </small>
          </li>
        ))}
      </ul>
    </section>
  )
}
