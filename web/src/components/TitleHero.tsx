import type { ReactNode } from 'react'
import type { CastMember, Trailer } from '../api'
import { trailerLabels } from '../trailerLabels'
import Icon from './Icon'
import { PosterFallback } from './PosterCard'

export interface HeroInfo {
  kind: 'movie' | 'tv'
  title: string
  year?: number
  posterUrl?: string
  genres?: string[]
  rating?: number
  voteCount?: number
  runtime?: number
  certification?: string
  tagline?: string
  overview?: string
  trailers?: Trailer[]
  cast?: CastMember[]
  facts?: { label: string; value: string }[]
  links?: { label: string; url: string }[] // the film's website, IMDb, TMDB
}

function runtimeLabel(min?: number): string {
  if (!min) return ''
  const h = Math.floor(min / 60)
  const m = min % 60
  return h ? `${h}h ${m}m` : `${m}m`
}

// The top of a movie or show page: poster on the left, everything worth
// knowing stacked on the right (title, rating, age rating, genres, summary,
// trailer links, cast), with the page's own action buttons passed in.
export default function TitleHero({ info, status, actions, children }: { info: HeroInfo; status?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  const trailers = (info.trailers ?? []).slice(0, 3)
  const trailerNames = trailerLabels(trailers)
  const facts = [
    info.year ? { label: 'Year', value: String(info.year) } : null,
    info.runtime ? { label: 'Runtime', value: runtimeLabel(info.runtime) } : null,
    ...(info.facts ?? []),
  ].filter(Boolean) as { label: string; value: string }[]

  return (
    <section className={`title-hero kind-${info.kind}`}>
      {info.posterUrl && <div className="title-hero-bg" style={{ backgroundImage: `url("${encodeURI(info.posterUrl).replace(/"/g, '%22')}")` }} aria-hidden="true" />}
      <div className="title-poster">{info.posterUrl ? <img src={info.posterUrl} alt={info.title} /> : <PosterFallback />}</div>
      <div className="title-info">
        <div className="title-kicker">
          <span className={`kind-tag kind-${info.kind}`}>
            <Icon name={info.kind === 'movie' ? 'film' : 'tv'} size={13} /> {info.kind === 'movie' ? 'Movie' : 'TV show'}
          </span>
          {status}
        </div>
        <h1>
          {info.title} {info.year ? <span className="title-year">({info.year})</span> : null}
        </h1>
        {info.tagline && <p className="title-tagline">{info.tagline}</p>}

        <div className="title-chips">
          {info.rating ? (
            <span className="chip-rate" title={info.voteCount ? `${info.voteCount.toLocaleString()} votes` : 'Rating'}>
              <Icon name="star" size={14} /> {info.rating.toFixed(1)}
              <small>/10</small>
            </span>
          ) : null}
          {info.certification && (
            <span className="chip-cert" title="Age classification">
              {info.certification}
            </span>
          )}
          {info.runtime ? (
            <span className="chip-plain">
              <Icon name="clock" size={13} /> {runtimeLabel(info.runtime)}
            </span>
          ) : null}
          {(info.genres ?? []).map((g) => (
            <span key={g} className="chip-genre">
              {g}
            </span>
          ))}
        </div>

        {info.overview && <p className="title-overview">{info.overview}</p>}

        {facts.length > 0 && (
          <dl className="title-facts">
            {facts.map((f) => (
              <div key={f.label}>
                <dt>{f.label}</dt>
                <dd>{f.value}</dd>
              </div>
            ))}
          </dl>
        )}

        {(trailers.length > 0 || (info.links ?? []).length > 0) && (
          <div className="title-links">
            {trailers.map((t, i) => (
              <a key={t.url} href={t.url} target="_blank" rel="noreferrer" className="link-pill">
                <Icon name="play" size={13} /> {trailerNames[i]}
              </a>
            ))}
            <a className="link-pill" href={`https://www.youtube.com/results?search_query=${encodeURIComponent(`${info.title} ${info.year ?? ''} review`)}`} target="_blank" rel="noreferrer">
              <Icon name="external" size={13} /> Reviews
            </a>
            {(info.links ?? []).map((l) => (
              <a key={l.url} className="link-pill" href={l.url} target="_blank" rel="noreferrer">
                <Icon name="external" size={13} /> {l.label}
              </a>
            ))}
          </div>
        )}

        {info.cast && info.cast.length > 0 && (
          <p className="title-cast">
            <strong>Starring</strong> {info.cast.slice(0, 6).map((c) => c.name).join(', ')}
          </p>
        )}

        {actions && <div className="title-actions">{actions}</div>}
        {children}
      </div>
    </section>
  )
}
