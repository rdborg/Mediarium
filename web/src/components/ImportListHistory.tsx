import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type ImportListHistory } from '../api'
import { timeAgo } from '../format'
import { t } from '../i18n'

// The History view of one import list (Settings > Import lists): what it has
// added, how much of that has a file now, how many downloads it started and
// what it skipped, so you can tell which lists are worth keeping.

const OUTCOMES: [string, string][] = [
  ['in-library', t('Already in the library')],
  ['existing', t('On the list when it was first read')],
  ['excluded', t('On your Not interested list')],
  ['filtered', t('Below the minimum rating or year')],
  ['no-match', t('Not found on TMDB')],
  ['failed', t("Couldn't be added")],
]

export default function ListHistory({ id }: { id: number }) {
  const [data, setData] = useState<ImportListHistory | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    api
      .importListHistory(id)
      .then((d) => live && setData(d))
      .catch((e) => live && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      live = false
    }
  }, [id])

  if (error) return <p className="error-text">{error}</p>
  if (!data) return <p>{t('Loading…')}</p>
  const skipped = OUTCOMES.filter(([k]) => (data.outcomes[k] ?? 0) > 0)
  return (
    <div className="il-history">
      <div className="il-stats">
        <div>
          <strong>{data.added}</strong>
          <span>{t('Titles added')}</span>
        </div>
        <div>
          <strong>{data.withFiles}</strong>
          <span>{t('Have a file now')}</span>
        </div>
        <div>
          <strong>{data.downloads}</strong>
          <span>{t('Downloads it started')}</span>
        </div>
        <div>
          <strong>{data.lastAddedAt ? timeAgo(data.lastAddedAt) : t('Never')}</strong>
          <span>{t('Last added')}</span>
        </div>
      </div>
      {skipped.length > 0 && (
        <p className="il-meta">
          {t('Skipped:')} {skipped.map(([k, label]) => `${label} ${data.outcomes[k]}`).join(' · ')}
        </p>
      )}
      {data.titles.length === 0 ? (
        <p className="il-meta">{t('This list has not added anything yet.')}</p>
      ) : (
        <table className="il-table">
          <thead>
            <tr>
              <th>{t('Title')}</th>
              <th>{t('Added')}</th>
              <th>{t('File')}</th>
            </tr>
          </thead>
          <tbody>
            {data.titles.map((x, i) => (
              <tr key={`${x.kind}-${x.tmdbId}-${i}`}>
                <td>
                  <Link to={x.kind === 'tv' ? `/show/${x.tmdbId}` : `/title/${x.tmdbId}`}>{x.title || `TMDB ${x.tmdbId}`}</Link>
                  {x.year ? <span className="il-year"> ({x.year})</span> : null}
                  <span className="badge il-kind">{x.kind === 'tv' ? t('TV show') : t('Movie')}</span>
                </td>
                <td title={new Date(x.at).toLocaleString()}>{timeAgo(x.at)}</td>
                <td>{x.hasFile ? t('Has a file') : x.libraryId ? t('No file yet') : t('No longer in the library')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {data.more && <p className="il-meta">{t('Only the newest {n} are shown.', { n: data.titles.length })}</p>}
    </div>
  )
}
