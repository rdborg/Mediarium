import { useCallback, useEffect, useState } from 'react'
import { api, can, type SeriesNumbering, type SeriesType } from '../api'
import { useAuth } from '../AuthContext'
import { locale, t, st } from '../i18n'
import { useToast } from './Toast'
import CustomNumbering from './CustomNumbering'

// What the numbering services say about a show: its AniList, AniDB and
// MyAnimeList pages, the other numbering releases use, and the other names it
// goes by. "Refresh mapping" reads it all again.
function when(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(locale(), { year: 'numeric', month: 'short', day: 'numeric' })
}

function sceneLine(n: SeriesNumbering): string {
  switch (n.sceneState) {
    case 'mapped':
      return n.sceneDiffers > 0
        ? t('TheXEM lists {total} episodes, {differ} of them numbered differently in release names.', { total: n.sceneRows, differ: n.sceneDiffers })
        : t('TheXEM lists {total} episodes, and release names number them the same way.', { total: n.sceneRows })
    case 'none':
      return t('TheXEM has no other numbering for this show.')
    case 'offline':
      return n.sceneRows > 0 ? t('TheXEM could not be reached. The numbering from the last time is used.') : t('TheXEM could not be reached.')
    default:
      return t('Not looked up yet.')
  }
}

function listsLine(n: SeriesNumbering): string {
  switch (n.listsState) {
    case 'found':
      return n.entries.length === 1 ? t('The anime lists have 1 entry for this show.') : t('The anime lists have {n} entries for this show.', { n: n.entries.length })
    case 'none':
      return t('The anime lists do not know this show.')
    case 'offline':
      return t('The anime lists could not be reached.')
    default:
      return t('Not looked up yet.')
  }
}

export default function NumberingCard({
  id,
  seriesType,
  seasons = [],
  monitored = true,
  onRescanned,
}: {
  id: number
  seriesType: SeriesType
  seasons?: number[]
  monitored?: boolean
  onRescanned?: () => void
}) {
  const toast = useToast()
  const me = useAuth().user
  const [n, setN] = useState<SeriesNumbering | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => {
    api
      .seriesNumbering(id)
      .then(setN)
      .catch(() => setN(null))
  }, [id])
  useEffect(load, [load])
  // A lookup that starts when an anime is added finishes a moment later.
  useEffect(() => {
    if (!n?.refreshing) return
    const timer = window.setTimeout(load, 4000)
    return () => window.clearTimeout(timer)
  }, [n, load])

  async function refresh() {
    setBusy(true)
    try {
      setN(await api.refreshSeriesNumbering(id))
      toast.success(t('Numbering and links refreshed.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!n) return null
  const anime = seriesType === 'anime'
  const known = n.sceneState !== '' || n.listsState !== ''
  // A show that is not an anime and has nothing found stays out of the way.
  if (!anime && !known) {
    return (
      <details className="card files-card" style={{ marginBottom: 24 }}>
        <summary>
          <h2 style={{ margin: 0, display: 'inline' }}>{t('Scene numbering')}</h2>
        </summary>
        <div style={{ marginTop: 14 }}>
          <p style={{ color: 'var(--text-dim)' }}>
            {t('Some releases number episodes differently from TMDB. Press the button to ask TheXEM and the anime lists about this show.')}
          </p>
          {can(me, 'manage') && (
            <button className="btn-with-icon" onClick={() => void refresh()} disabled={busy}>
              {busy ? t('Refreshing…') : t('Refresh mapping')}
            </button>
          )}
          <CustomNumbering id={id} seasons={seasons} monitored={monitored} onRescanned={onRescanned} />
        </div>
      </details>
    )
  }

  const links = [
    n.anilistUrl && { key: 'anilist', label: 'AniList', url: n.anilistUrl },
    n.anidbUrl && { key: 'anidb', label: 'AniDB', url: n.anidbUrl },
    n.malUrl && { key: 'mal', label: 'MyAnimeList', url: n.malUrl },
  ].filter(Boolean) as { key: string; label: string; url: string }[]

  return (
    <section className="card numbering-card" style={{ marginBottom: 24 }}>
      <div className="numbering-head">
        <h2 style={{ margin: 0 }}>{anime ? t('Anime numbering and links') : t('Scene numbering')}</h2>
        {links.length > 0 && (
          <span className="numbering-links">
            {links.map((l) => (
              <a key={l.key} className="chip" href={l.url} target="_blank" rel="noreferrer">
                {l.label}
              </a>
            ))}
          </span>
        )}
        {can(me, 'manage') && (
          <button className="btn-with-icon numbering-refresh" onClick={() => void refresh()} disabled={busy || !!n.refreshing}>
            {busy || n.refreshing ? t('Refreshing…') : t('Refresh mapping')}
          </button>
        )}
      </div>
      <ul className="numbering-facts">
        <li>{sceneLine(n)}</li>
        <li>{listsLine(n)}</li>
        {n.names.length > 0 && (
          <li>
            {t('Also known as')}: {n.names.join(' · ')}
          </li>
        )}
      </ul>
      {n.entries.length > 1 && (
        <details>
          <summary>{t('Entries in the anime lists')}</summary>
          <table className="numbering-entries">
            <thead>
              <tr>
                <th>{t('Name')}</th>
                <th>{t('Season')}</th>
                <th>{t('Episode 1 is')}</th>
                <th>{t('Links')}</th>
              </tr>
            </thead>
            <tbody>
              {n.entries.map((e) => (
                <tr key={e.anidb}>
                  <td>{e.name || `AniDB ${e.anidb}`}</td>
                  <td>{e.season > 0 ? e.season : e.season === 0 ? t('Specials') : '—'}</td>
                  <td>{e.season > 0 ? t('episode {n}', { n: 1 + (e.offset ?? 0) }) : '—'}</td>
                  <td>
                    {e.anilist ? (
                      <a href={`https://anilist.co/anime/${e.anilist}`} target="_blank" rel="noreferrer">
                        AniList
                      </a>
                    ) : null}{' '}
                    <a href={`https://anidb.net/anime/${e.anidb}`} target="_blank" rel="noreferrer">
                      AniDB
                    </a>{' '}
                    {e.mal ? (
                      <a href={`https://myanimelist.net/anime/${e.mal}`} target="_blank" rel="noreferrer">
                        MyAnimeList
                      </a>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      )}
      <CustomNumbering id={id} seasons={seasons} monitored={monitored} onRescanned={onRescanned} />
      {n.note && <p className="numbering-note">{st(n.note)}</p>}
      <p className="numbering-foot">
        {n.checkedAt ? t('Last looked up {date}.', { date: when(n.checkedAt) }) : t('Not looked up yet.')}{' '}
        {anime && !n.lookup ? t('Automatic lookups are off in Settings > Library and files > Movie and show info, so this only changes when you press the button.') : ''}
      </p>
    </section>
  )
}
