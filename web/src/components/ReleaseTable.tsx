import { Fragment, useMemo, useState } from 'react'
import type { SearchResult } from '../api'
import { formatBytes, nameParts } from '../format'
import { locale, st, t } from '../i18n'
import { loadShowWhy, saveShowWhy, signed, whyOf, type WhyInfo } from '../releaseWhy'
import { indexerChoices, type Searched } from '../searched'
import { FIRST_DIRECTION, ageOf, defaultOrder, filterReleases, publishedAt, qualitiesIn, scoreOf, sortReleases, type ReleaseSortKey, type SortDir } from '../releaseSort'
import Dropdown from './Dropdown'
import Icon from './Icon'
import ReleaseChecks from './ReleaseChecks'
import TorrentsOffNote from './TorrentsOffNote'

// Interactive-search results: every release with quality info, badges for
// packs / blocklisted / "automation would skip this" (with the reason on
// hover), and a Grab button — grabbing a rejected release by hand is always
// allowed. The columns sort when you click their names, the list can be
// narrowed by indexer, quality or words, and the card can be closed.
export default function ReleaseTable({
  results,
  grabbing,
  onGrab,
  onClose,
  showPackBadge = false,
  qualityLabel,
  qualityGroup,
  searched,
}: {
  results: SearchResult[]
  grabbing: string | null
  onGrab: (r: SearchResult) => void
  // Gives the card a Close button.
  onClose?: () => void
  showPackBadge?: boolean
  // What to show in the Quality column when a release has no resolution (music).
  qualityLabel?: (r: SearchResult) => string | undefined
  // What the Quality filter groups by when releases have no resolution (music: FLAC, MP3 320 ...). Without it the filter uses the resolution.
  qualityGroup?: (r: SearchResult) => string | undefined
  // What each indexer did: the indexer filter then lists all of them.
  searched?: Searched[]
}) {
  const showLanguage = results.some((r) => r.language)
  const showScore = results.some((r) => scoreOf(r) !== undefined)
  const [sort, setSort] = useState<{ key: ReleaseSortKey; dir: SortDir } | null>(null)
  const [words, setWords] = useState('')
  const [indexer, setIndexer] = useState('')
  const [quality, setQuality] = useState('')
  // "Show why" opens the details of every row; a row's own button overrides it.
  const [showWhy, setShowWhy] = useState(loadShowWhy)
  const [opened, setOpened] = useState<Record<string, boolean>>({})
  const indexers = useMemo(() => indexerChoices(results, searched), [results, searched])
  const groupOf = (r: SearchResult) => (qualityGroup ? qualityGroup(r) || 'other' : '')
  const qualities = useMemo(() => {
    if (!qualityGroup) return qualitiesIn(results)
    const counts = new Map<string, number>()
    for (const r of results) counts.set(groupOf(r), (counts.get(groupOf(r)) ?? 0) + 1)
    return [...counts.keys()].sort((a, b) => (a === 'other' ? 1 : b === 'other' ? -1 : (counts.get(b) ?? 0) - (counts.get(a) ?? 0) || a.localeCompare(b)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [results, qualityGroup])
  const shown = useMemo(() => {
    const base = qualityGroup ? filterReleases(results, { text: words, indexer, quality: '' }).filter((r) => !quality || groupOf(r) === quality) : filterReleases(results, { text: words, indexer, quality })
    return sortReleases(defaultOrder(base), sort?.key ?? null, sort?.dir ?? 1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [results, words, indexer, quality, sort, qualityGroup])
  const filtering = words !== '' || indexer !== '' || quality !== ''
  // Two releases can share one link (the same torrent listed twice), and React mixes up rows that share a key: a row the filter removed could stay on screen.
  const rowKeys = useMemo(() => {
    const seen = new Map<string, number>()
    return shown.map((r) => {
      const n = seen.get(r.downloadUrl) ?? 0
      seen.set(r.downloadUrl, n + 1)
      return n === 0 ? r.downloadUrl : `${r.downloadUrl}#${n}`
    })
  }, [shown])

  // First click sorts the natural way for that column, the second reverses it, the third goes back to the starting order (best match first, then by score).
  function clickSort(key: ReleaseSortKey) {
    setSort((cur) => {
      if (!cur || cur.key !== key) return { key, dir: FIRST_DIRECTION[key] }
      if (cur.dir === FIRST_DIRECTION[key]) return { key, dir: (-FIRST_DIRECTION[key]) as SortDir }
      return null
    })
  }
  const head = (key: ReleaseSortKey, label: string, title?: string) => (
    <th aria-sort={sort?.key === key ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'} title={title}>
      <button type="button" className="th-sort" onClick={() => clickSort(key)}>
        {label}
        <span className="th-arrow" aria-hidden="true">
          {sort?.key === key ? (sort.dir === 1 ? '▲' : '▼') : ''}
        </span>
      </button>
    </th>
  )
  const qualityName = (q: string) => (q === 'other' ? (qualityGroup ? t('Unknown') : t('No resolution')) : q)
  const anyWhy = useMemo(() => results.some((r) => whyOf(r) !== null), [results])
  const columns = 7 + (showLanguage ? 1 : 0) + (showScore ? 1 : 0)
  function toggleAll() {
    const next = !showWhy
    setShowWhy(next)
    setOpened({})
    saveShowWhy(next)
  }
  const whyButton = (r: SearchResult, why: WhyInfo, label: string) => {
    const open = opened[r.downloadUrl] ?? showWhy
    return (
      <button type="button" className="rel-why-btn" aria-expanded={open} aria-label={label} title={label} onClick={() => setOpened((o) => ({ ...o, [r.downloadUrl]: !open }))}>
        <span aria-hidden="true">{open ? '▾' : '▸'}</span>
        {why.reasons.length > 0 && <span className="rel-why-dot" aria-hidden="true" />}
      </button>
    )
  }

  return (
    <>
      <TorrentsOffNote results={results} />
      <div className="rel-tools">
        <input type="search" value={words} onChange={(e) => setWords(e.target.value)} placeholder={t('Find in these releases')} aria-label={t('Find in these releases')} />
        {indexers.length > 1 && (
          <Dropdown
            label={t('Indexer')}
            value={indexer}
            onChange={setIndexer}
            options={[{ value: '', label: t('Any indexer') }, ...indexers.map((i) => ({ value: i.value, label: `${i.label} (${i.count})`, dim: i.count === 0, warn: i.failed }))]}
          />
        )}
        {qualities.length > 1 && <Dropdown label={t('Quality')} value={quality} onChange={setQuality} options={[{ value: '', label: t('Any quality') }, ...qualities.map((q) => ({ value: q, label: qualityName(q) }))]} />}
        {anyWhy && (
          <button type="button" className="btn-sm" aria-pressed={showWhy} onClick={toggleAll} title={t('Open the details under every release: where the score comes from and why an automatic search may skip it')}>
            {t('Show why')}
          </button>
        )}
        <span className="rel-count">{t('{shown} of {total}', { shown: shown.length, total: results.length })}</span>
        {filtering && (
          <button
            type="button"
            className="btn-sm"
            onClick={() => {
              setWords('')
              setIndexer('')
              setQuality('')
            }}
          >
            {t('Clear filters')}
          </button>
        )}
        {onClose && (
          <button type="button" className="btn-sm btn-with-icon rel-close" onClick={onClose}>
            <Icon name="x" size={14} /> {t('Close')}
          </button>
        )}
      </div>
      {shown.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing matches these filters.')}</p>}
      {shown.length > 0 && (
        <div className="rel-scroll">
          <table className="rel-table">
            <thead>
              <tr>
                {head('release', t('Release'))}
                {head('quality', t('Quality'))}
                {showLanguage && head('language', t('Language'))}
                {showScore && head('score', t('Score'), t("Score in this title's quality profile: custom formats plus preferred terms"))}
                {head('indexer', t('Indexer'))}
                {head('size', t('Size'))}
                {head('age', t('Age'), t('How long ago the indexer posted it'))}
                {head('seeders', t('Seeders'))}
                <th></th>
              </tr>
            </thead>
            <tbody>
              {shown.map((r, i) => {
                const why = whyOf(r)
                const open = why !== null && (opened[r.downloadUrl] ?? showWhy)
                return (
                <Fragment key={rowKeys[i]}>
                <tr className={open ? 'rel-open' : undefined} style={r.rejections?.length || r.blocklisted ? { opacity: 0.75 } : undefined}>
                  <td className="rel-name">
                    {nameParts(r.title).map((p, i) => (
                      <Fragment key={i}>
                        {p}
                        <wbr />
                      </Fragment>
                    ))}
                    {r.best && (
                      <span className="badge downloaded" style={{ marginLeft: 8 }} title={t('The release an automatic search would take')}>
                        {t('Best match')}
                      </span>
                    )}
                    {r.matchedTitle && <div className="lang-hint lang-hint-wrap">{t('Matched on the title "{title}"', { title: r.matchedTitle })}</div>}
                    {r.categories && r.categories.length > 0 && <div className="lang-hint lang-hint-wrap">{t('Category {numbers}', { numbers: r.categories.join(', ') })}</div>}
                    {showPackBadge && (r.episodes?.length ?? 0) === 0 && (
                      <span className="badge" style={{ marginLeft: 8 }}>
                        {t('season pack')}
                      </span>
                    )}
                    {r.cached && (
                      <span className="badge downloaded" style={{ marginLeft: 8 }} title={t('Your debrid service already has this torrent, so it starts at once')}>
                        {t('Cached')}
                      </span>
                    )}
                    {r.belowMinSeeders && (
                      <span className="badge conflict" style={{ marginLeft: 8 }} title={t('This release has fewer seeders than the minimum of {n} set for its indexer. Automatic downloads leave it out. You can still grab it yourself.', { n: r.minSeeders ?? 0 })}>
                        {t('Below minimum seeders')}
                      </span>
                    )}
                    {r.blocklisted && (
                      <span className="badge failed" style={{ marginLeft: 8 }} title={t('This release failed before')}>
                        {t('blocklisted')}
                      </span>
                    )}
                    {r.acceptedBy?.fallback && (
                      <span className="badge fallback-badge" style={{ marginLeft: 8 }} title={t('Only used if nothing is found at the title\'s own quality: accepted by the "{name}" fallback', { name: r.acceptedBy.profileName })}>
                        {t('fallback: {name}', { name: r.acceptedBy.profileName })}
                      </span>
                    )}
                    {r.rejections && r.rejections.length > 0 && !r.acceptedBy?.fallback && (
                      <span className="badge conflict" style={{ marginLeft: 8 }} title={r.rejections.join('\n')}>
                        {r.rejections[0]}
                      </span>
                    )}
                    {!showScore && why && whyButton(r, why, t('Details for this release'))}
                  </td>
                  <td className="rel-fit">{qualityLabel?.(r) || [r.resolution, r.source, r.codec].filter(Boolean).join(' · ') || '—'}</td>
                  {showLanguage && (
                    <td className="rel-fit">
                      {r.language ? (
                        <span
                          className={r.languageFit === 'other' ? 'badge conflict' : 'lang-hint'}
                          title={
                            r.languageFit === 'other'
                              ? t('Automatic downloads skip releases in another language. You can still grab it yourself.')
                              : r.languageFit === 'mixed'
                                ? t('Has another language as well. Automatic downloads take a plain release first.')
                                : undefined
                          }
                        >
                          {r.language}
                        </span>
                      ) : (
                        '—'
                      )}
                    </td>
                  )}
                  {showScore && (
                    <td className={`rel-fit format-score-cell${(scoreOf(r) ?? 0) < 0 ? ' neg' : ''}`} title={scoreTip(r)}>
                      {scoreOf(r) === undefined ? '—' : signed(scoreOf(r) ?? 0)}
                      {why && whyButton(r, why, t('Why this score'))}
                    </td>
                  )}
                  <td className="rel-fit">
                    <span className="badge" title={r.protocol}>
                      {r.protocol === 'torrent' ? '⇅' : r.protocol === 'soulseek' ? '♫' : '⇩'} {r.indexerName}
                    </span>
                  </td>
                  <td className="rel-fit">{formatBytes(r.sizeBytes)}</td>
                  <td className="rel-fit" title={r.publishDate && publishedAt(r) ? new Date(publishedAt(r)).toLocaleString(locale()) : undefined}>{ageOf(r) || '—'}</td>
                  <td className={`rel-fit${r.belowMinSeeders ? ' rel-dim' : ''}`}>{r.protocol === 'torrent' ? `${r.seeders ?? 0} / ${r.peers ?? 0}` : '—'}</td>
                  <td className="rel-fit">
                    <button className="primary" disabled={grabbing !== null} onClick={() => onGrab(r)}>
                      {grabbing === r.downloadUrl ? t('Grabbing…') : t('Grab')}
                    </button>
                  </td>
                </tr>
                {open && why && (
                  <tr className="rel-why-row">
                    <td colSpan={columns}>
                      <WhyDetails why={why} />
                    </td>
                  </tr>
                )}
                </Fragment>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}

// scoreTip lists the parts of a release's score for the tooltip of its cell.
function scoreTip(r: SearchResult): string {
  const lines = [
    ...(r.formats ?? []).map((f) => `${f.name} ${signed(f.score)}`),
    ...(r.terms ?? []).map((x) => `${x.term} ${signed(x.score)}`),
    ...(r.fixScore ? [`${t('Fixed release (PROPER or REPACK)')} ${signed(r.fixScore)}`] : []),
  ]
  return lines.length ? lines.join('\n') : t('No custom format matches')
}

// The details under a release: where the score comes from, and everything that
// makes an automatic search skip it. Grabbing it by hand always works.
function WhyDetails({ why }: { why: WhyInfo }) {
  return (
    <div className="rel-why">
      {why.checks.length > 0 && (
        <div className="rel-why-col">
          <h4>{t('Why this release?')}</h4>
          <ReleaseChecks checks={why.checks} />
        </div>
      )}
      {(why.score !== undefined || why.terms.length > 0 || why.fix !== 0) && (
        <div className="rel-why-col">
          <h4>{t('Why this score')}</h4>
          {why.formats.length + why.terms.length + (why.fix !== 0 ? 1 : 0) > 0 ? (
            <ul className="rel-why-formats">
              {why.formats.map((f) => (
                <li key={`f${f.name}`}>
                  <span>{f.name}</span>
                  <span className={`format-score-cell${f.score < 0 ? ' neg' : ''}`}>{signed(f.score)}</span>
                </li>
              ))}
              {why.terms.map((x) => (
                <li key={`t${x.term}`}>
                  <span>{x.term}</span>
                  <span className={`format-score-cell${x.score < 0 ? ' neg' : ''}`}>{signed(x.score)}</span>
                </li>
              ))}
              {why.fix !== 0 && (
                <li>
                  <span>{t('Fixed release (PROPER or REPACK)')}</span>
                  <span className="format-score-cell">{signed(why.fix)}</span>
                </li>
              )}
            </ul>
          ) : (
            <p className="rel-why-none">{t('No custom format matches')}</p>
          )}
          <p className="rel-why-total">
            <span>{t('Total')}</span>
            <span className={`format-score-cell${why.total < 0 ? ' neg' : ''}`}>{signed(why.total)}</span>
            {why.belowMin && <span className="badge conflict">{t('below the minimum')}</span>}
          </p>
          {why.score !== undefined && (
            <p className="rel-why-limits">
              {t('Minimum score: {n}', { n: signed(why.minScore) })}
              {why.upgradeUntil > 0 && ` · ${t('Upgrades stop at: {n}', { n: signed(why.upgradeUntil) })}`}
            </p>
          )}
        </div>
      )}
      {why.rank.length > 0 && (
        <div className="rel-why-col">
          <h4>{t('Where it ranks')}</h4>
          <ul className="rel-why-reasons">
            {why.rank.map((text, i) => (
              <li key={i} className="rel-why-rank">
                {st(text)}
              </li>
            ))}
          </ul>
        </div>
      )}
      {why.notBest.length > 0 && (
        <div className="rel-why-col">
          <h4>{t('Why it was not the best match')}</h4>
          <ul className="rel-why-reasons">
            {why.notBest.map((text, i) => (
              <li key={i} className="rel-why-notBest">
                {st(text)}
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="rel-why-col">
        <h4>{t('Why automatic searches skip it')}</h4>
        {why.reasons.length > 0 ? (
          <ul className="rel-why-reasons">
            {why.reasons.map((r, i) => (
              <li key={i} className={`rel-why-${r.kind}`}>
                {st(r.text)}
              </li>
            ))}
          </ul>
        ) : (
          <p className="rel-why-none">{t('Nothing here makes an automatic search skip it.')}</p>
        )}
      </div>
    </div>
  )
}
