import { useState } from 'react'
import { Link } from 'react-router-dom'
import { bookReleaseHint, releaseHint, skippedHint, type Unanswered } from '../releaseHint'
import { resultText, seconds, summaryLine, type Searched } from '../searched'
import { st, t } from '../i18n'
import Icon from './Icon'

// "Searched 8 indexers: 2 had releases, 5 had nothing for this title, 1 did
// not answer." with a button that opens the table behind it: one row for each
// indexer, with what it found and how long it took.
function SearchedSummary({ searched, className }: { searched: Searched[]; className?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div className={`searched${className ? ` ${className}` : ''}`}>
      <button type="button" className="searched-toggle" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <Icon name="chevron-down" size={15} className="searched-chev" />
        <span>{summaryLine(searched)}</span>
        <span className="searched-more">{open ? t('Hide details') : t('Show details')}</span>
      </button>
      {open && (
        <div className="searched-scroll">
          <table className="searched-table">
            <thead>
              <tr>
                <th>{t('Indexer')}</th>
                <th>{t('Type')}</th>
                <th>{t('Result')}</th>
                <th className="num">{t('Time')}</th>
              </tr>
            </thead>
            <tbody>
              {searched.map((s) => (
                <tr key={s.name} className={`searched-${s.status}`}>
                  <td>{s.name}</td>
                  <td>{s.kind === 'usenet' ? t('Usenet') : s.kind === 'torrent' ? t('Torrent') : s.kind === 'soulseek' ? t('Soulseek') : ''}</td>
                  <td title={s.reason ? [st(s.reason), s.detail].filter(Boolean).join('\n') : undefined}>
                    {s.status === 'failed' && <Icon name="warning" size={14} className="searched-warn" />}
                    {resultText(s)}
                    {s.status === 'failed' && s.reason && (s.code === 'flaresolverr' || s.code === 'gone') && <small className="searched-reason">{st(s.reason)}</small>}
                    {s.status === 'failed' && s.detail && <small className="searched-detail">{s.detail}</small>}
                  </td>
                  <td className="num">{seconds(s.ms)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

// The line about the indexers next to a release list: says why there is
// nothing to show, or that some sources did not answer. For a book search,
// skipped names the indexers that were left out because they don't do books.
export default function ReleaseNotice({
  sources,
  unanswered,
  warnings,
  results,
  className,
  skipped,
  searched,
  ignoredSamples,
}: {
  sources: number
  unanswered: Unanswered[]
  warnings?: string[]
  results: number
  className?: string
  skipped?: string[]
  // What each indexer did. When it is there, the summary says it all: who did
  // not answer, who was left out, and the daily-limit warnings fold into it.
  searched?: Searched[]
  // Names of releases that came back but are not this title (see releaseHint).
  ignoredSamples?: string[]
}) {
  const summary = searched && searched.length > 0 ? searched : null
  const h0 = skipped ? bookReleaseHint(sources, unanswered, results, skipped) : releaseHint(sources, unanswered, results, ignoredSamples)
  // With the summary, a list that has releases does not repeat who did not answer.
  const h = summary && h0 && !h0.empty ? null : h0
  const left = skipped && !summary ? skippedHint(skipped) : null
  const shownWarnings = summary ? undefined : warnings
  if (!h && !shownWarnings?.length && !left && !summary) return null
  return (
    <>
      {summary && <SearchedSummary searched={summary} className={className} />}
      {h && (
        <p className={className} style={{ color: 'var(--text-dim)' }} role={h.empty ? undefined : 'status'}>
          {h.text}
          {h.link && (
            <>
              {' '}
              <Link to="/settings/indexers">{t('Settings > Indexers & Search')}</Link>.
            </>
          )}
        </p>
      )}
      {left && (
        <p className={className} style={{ color: 'var(--text-dim)' }} role="status">
          {left}
        </p>
      )}
      {shownWarnings?.map((w) => (
        <p key={w} className={className} style={{ color: 'var(--text-dim)' }} role="status">
          {st(w)}
        </p>
      ))}
    </>
  )
}
