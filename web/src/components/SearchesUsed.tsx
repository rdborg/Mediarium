import { st, t } from '../i18n'
import type { Searched } from '../searched'

// What a release search asked and what came of it: for each indexer the words
// it was sent (with the categories), how many releases it sent back for them,
// and the releases that were left out or marked as ones automation would skip,
// with the reason. It sits closed under the summary line of a search.
export interface Filtered {
  reason: string
  count: number
}

// reasonText puts a server reason in the page's language.
export function reasonText(reason: string): string {
  return reason === 'not this title' ? t('not this title') : st(reason)
}

export default function SearchesUsed({ searched, filtered, className }: { searched: Searched[]; filtered?: Filtered[]; className?: string }) {
  const rows = searched.flatMap((s) => (s.queries ?? []).map((q) => ({ indexer: s.name, ...q })))
  if (rows.length === 0 && !filtered?.length) return null
  return (
    <details className={`searches-used${className ? ` ${className}` : ''}`}>
      <summary>{t('Searches used')}</summary>
      {rows.length > 0 && (
        <div className="searched-scroll">
          <table className="searched-table">
            <thead>
              <tr>
                <th>{t('Indexer')}</th>
                <th>{t('What was asked')}</th>
                <th className="num">{t('Releases')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={`${r.indexer}:${r.query}:${i}`} className={r.failed ? 'searched-failed' : undefined}>
                  <td>{r.indexer}</td>
                  <td>
                    <code>{r.query}</code>
                    {r.categories && r.categories.length > 0 && <small className="searches-used-cats"> {t('categories {list}', { list: r.categories.join(', ') })}</small>}
                  </td>
                  <td className="num">{r.failed ? t('no answer') : r.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {filtered && filtered.length > 0 && (
        <div className="searches-used-filtered">
          <strong>{t('Left out or marked as skipped')}</strong>
          <ul>
            {filtered.map((f) => (
              <li key={f.reason}>
                {reasonText(f.reason).replace(/[.\u3002]$/, '')}: {f.count}
              </li>
            ))}
          </ul>
        </div>
      )}
    </details>
  )
}
