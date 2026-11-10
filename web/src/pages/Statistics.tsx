import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, isAdmin, type IndexerStat, type ServerTraffic, type UsenetMonth, type LibraryStats, type WatchStats, type WatchedStatTitle } from '../api'
import { useAuth } from '../AuthContext'
import { formatBytes, timeAgo } from '../format'
import KindGlyph from '../components/KindGlyph'
import Loading from '../components/Loading'
import OverTime from '../components/OverTime'
import { locale, plural, t } from '../i18n'
import { useConfirm } from '../components/ConfirmProvider'
import { useToast } from '../components/Toast'
import '../stats.css'
import TipLabel from '../components/TipLabel'

// How much of a kind is in, as a ring around its shape.
function Ring({ pct }: { pct: number }) {
  return (
    <svg className="stat-ring" viewBox="0 0 44 44" aria-hidden="true">
      <circle cx="22" cy="22" r="19" pathLength={100} className="track" />
      <circle cx="22" cy="22" r="19" pathLength={100} className="fill" strokeDasharray={`${Math.max(pct, 0.5)} 100`} />
    </svg>
  )
}

// Statistics: what is in the library, at which quality, how much space it
// takes, and how downloads went month by month.
export default function Statistics() {
  const [s, setS] = useState<LibraryStats | null>(null)
  const [error, setError] = useState('')
  const admin = isAdmin(useAuth().user)
  const confirm = useConfirm()
  const toast = useToast()
  const load = () =>
    api
      .libraryStats()
      .then(setS)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  useEffect(() => {
    void load()
  }, [])

  // Starts the download numbers again from zero. Nothing else is touched.
  async function reset() {
    const ok = await confirm({
      title: t('Reset the statistics?'),
      body: (
        <p>
          {t('The download numbers on this page and on the dashboard start again from zero: how many downloads were grabbed, finished and failed, how big they were, and the share from Usenet. Your library, your history and your files are not touched.')}
        </p>
      ),
      confirmLabel: t('Reset statistics'),
      danger: true,
    })
    if (!ok) return
    try {
      await api.resetStats()
      toast.success(t('Statistics reset.'))
      await load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (error) return <p className="error-text">{error}</p>
  if (!s) return <Loading height={300} />
  const qualityMax = Math.max(1, ...s.quality.map((q) => q.count))
  const monthMax = Math.max(1, ...s.months.map((m) => m.completed + m.failed))
  const pct = (a: number, b: number) => (b > 0 ? Math.round((a * 100) / b) : 0)
  return (
    <div className="stats-page">
      <div className="page-header">
        <h1>{t('Statistics')}</h1>
      </div>
      <div className="stat-cards">
        <div className="card stat-card k-movie">
          <span className="stat-shape">
            <Ring pct={pct(s.moviesHave, s.movies)} />
            <KindGlyph kind="movie" size={26} variant="solid" />
          </span>
          <small>{t('Movies')}</small>
          <strong>{s.movies}</strong>
          <span>
            {t('{n} downloaded ({pct}%)', { n: s.moviesHave, pct: pct(s.moviesHave, s.movies) })} · {formatBytes(s.movieBytes)}
          </span>
        </div>
        <div className="card stat-card k-tv">
          <span className="stat-shape">
            <Ring pct={pct(s.episodesHave, s.episodes)} />
            <KindGlyph kind="tv" size={26} variant="solid" />
          </span>
          <small>{t('TV shows')}</small>
          <strong>{s.shows}</strong>
          <span>
            {t('{have} of {total} episodes ({pct}%)', { have: s.episodesHave, total: s.episodes, pct: pct(s.episodesHave, s.episodes) })} · {formatBytes(s.episodeBytes)}
          </span>
        </div>
        {s.albums > 0 && (
          <div className="card stat-card k-music">
            <span className="stat-shape">
              <Ring pct={pct(s.albumsHave, s.albums)} />
              <KindGlyph kind="music" size={26} variant="solid" />
            </span>
            <small>{t('Albums')}</small>
            <strong>{s.albums}</strong>
            <span>
              {t('{n} downloaded ({pct}%)', { n: s.albumsHave, pct: pct(s.albumsHave, s.albums) })}
            </span>
          </div>
        )}
        <div className="card stat-card k-size">
          <small>{t('Library size')}</small>
          <strong>{formatBytes(s.movieBytes + s.episodeBytes)}</strong>
          <span>{s.usenetShare > 0 || s.months.length > 0 ? t('{pct}% of downloads came from Usenet', { pct: s.usenetShare }) : t('No downloads yet')}</span>
          {(s.usenetShare > 0 || s.months.length > 0) && (
            <span className="stat-split" aria-hidden="true">
              <i style={{ width: `${s.usenetShare}%` }} />
            </span>
          )}
        </div>
      </div>

      <OverTime />

      <div className="half-cols">
        <section className="card">
          <h2>{t('By quality')}</h2>
          {s.quality.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing downloaded yet.')}</p>}
          <div className="bar-list">
            {s.quality.map((q) => (
              <div key={q.label} className="bar-row">
                <span className="bar-label">{q.label}</span>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(q.count * 100) / qualityMax}%` }} />
                </span>
                <span className="bar-value">
                  {q.count} · {formatBytes(q.bytes ?? 0)}
                </span>
              </div>
            ))}
          </div>
        </section>
        <section className="card">
          <h2>{t('Downloads by month')}</h2>
          {s.months.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('No downloads counted yet.')}</p>}
          <div className="bar-list">
            {s.months.map((m) => (
              <div key={m.month} className="bar-row">
                <span className="bar-label">{new Date(`${m.month}-15`).toLocaleDateString(locale(), { month: 'short', year: 'numeric' })}</span>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(m.completed * 100) / monthMax}%` }} />
                  <span className="bar-fill bad" style={{ width: `${(m.failed * 100) / monthMax}%` }} />
                </span>
                <span className="bar-value">
                  {m.failed > 0 ? t('{done} done, {failed} failed', { done: m.completed, failed: m.failed }) : t('{done} done', { done: m.completed })} · {formatBytes(m.bytes)}
                </span>
              </div>
            ))}
          </div>
          {(s.totals.grabs > 0 || s.totals.completed > 0 || s.totals.failed > 0) && (
            <p className="stats-totals">
              <b>{t('{grabbed} grabbed, {done} done, {failed} failed, {size} downloaded', { grabbed: s.totals.grabs, done: s.totals.completed, failed: s.totals.failed, size: formatBytes(s.totals.bytes) })}</b>
            </p>
          )}
          <p className="field-hint">{t('These numbers are counted as downloads happen, so clearing the history list does not change them.')}</p>
          {admin && (
            <button type="button" className="btn-sm btn-danger" style={{ marginTop: 10 }} onClick={() => void reset()}>
              {t('Reset statistics')}
            </button>
          )}
        </section>
      </div>
      <IndexerStats />
      {s.watched && <WatchedStats w={s.watched} />}
    </div>
  )
}

// How each indexer has answered over the last days: searches, failures, the
// share that worked, releases found and grabs. Administrators only, because it
// names the indexers.
function IndexerStats() {
  const admin = isAdmin(useAuth().user)
  const [data, setData] = useState<{ days: number; indexers: IndexerStat[]; servers?: ServerTraffic[] } | null>(null)
  useEffect(() => {
    if (admin) api.indexerStats().then(setData).catch(() => undefined)
  }, [admin])
  if (!admin || !data) return null
  const busiest = Math.max(1, ...data.indexers.map((i) => i.queries))
  const servers = (data.servers ?? []).filter((s) => s.enabled || s.bytes > 0)
  return (
    <>
    {servers.length > 0 && <ServerTrafficStats days={data.days} servers={servers} />}
    {servers.length > 0 && <UsenetMonthsStats servers={servers} />}
    {data.indexers.length > 0 && (
    <section className="card" style={{ marginTop: 16 }}>
      <h2><TipLabel help="indexers#how-each-indexer-is-doing">{plural(data.days, 'Indexers, last {n} day', 'Indexers, last {n} days')}</TipLabel></h2>
      <div style={{ overflowX: 'auto' }}>
        <table className="indexer-stats" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ textAlign: 'left' }}>{t('Indexer')}</th>
              <th style={{ textAlign: 'right' }}>{t('Searches')}</th>
              <th style={{ textAlign: 'right' }}>{t('Failed')}</th>
              <th style={{ textAlign: 'right' }}>{t('Worked')}</th>
              <th style={{ textAlign: 'right' }}>{t('Releases found')}</th>
              <th style={{ textAlign: 'right' }}>{t('Grabbed')}</th>
            </tr>
          </thead>
          <tbody>
            {data.indexers.map((i) => (
              <tr key={i.id} style={{ opacity: i.enabled ? 1 : 0.55 }}>
                <td>
                  <strong>{i.name}</strong> <span className="badge">{i.protocol === 'torrent' ? t('Torrent') : t('Usenet')}</span>
                  {!i.enabled && <small> · {t('Off')}</small>}
                  {i.pausedUntil && <small> · {t('paused')}</small>}
                  <span className="bar-track" style={{ display: 'block', marginTop: 4, maxWidth: 220 }}>
                    <span className="bar-fill" style={{ width: `${(i.queries * 100) / busiest}%` }} />
                  </span>
                </td>
                <td style={{ textAlign: 'right' }}>{i.queries}</td>
                <td style={{ textAlign: 'right' }}>{i.failures}</td>
                <td style={{ textAlign: 'right' }}>{i.successRate < 0 ? '–' : `${Math.round(i.successRate * 100)}%`}</td>
                <td style={{ textAlign: 'right' }}>{i.results}</td>
                <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                  {i.grabs}
                  {(i.grabBytes ?? 0) > 0 && <small className="mono" style={{ color: 'var(--text-dim)' }}> · {formatBytes(i.grabBytes ?? 0)}</small>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="field-hint">{t('A search is one question to one indexer. Grabs count the releases taken from it, by hand or by Mediarium, with their size. An indexer that is paused after repeated failures is not asked, and those skipped searches are not counted.')}</p>
    </section>
    )}
    </>
  )
}

// What each Usenet provider delivered over the same days, so you can see which
// paid plan or block account does the work (#167). Only the built-in
// downloader counts: SABnzbd and NZBGet don't say which server served what.
function ServerTrafficStats({ days, servers }: { days: number; servers: ServerTraffic[] }) {
  const most = Math.max(1, ...servers.map((s) => s.bytes))
  return (
    <section className="card" style={{ marginTop: 16 }}>
      <h2><TipLabel help="downloads#how-much-each-provider-delivered">{plural(days, 'Usenet providers, last {n} day', 'Usenet providers, last {n} days')}</TipLabel></h2>
      <div style={{ overflowX: 'auto' }}>
        <table className="indexer-stats" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ textAlign: 'left' }}>{t('Server')}</th>
              <th style={{ textAlign: 'right' }}>{t('Downloaded')}</th>
              <th style={{ textAlign: 'right' }}>{t('Articles')}</th>
              <th style={{ textAlign: 'right' }}>{t('Missing')}</th>
            </tr>
          </thead>
          <tbody>
            {servers.map((s) => {
              const asked = s.articles + s.missing
              return (
                <tr key={s.id} style={{ opacity: s.enabled ? 1 : 0.55 }}>
                  <td>
                    <strong>{s.name}</strong> <small className="mono" style={{ color: 'var(--text-dim)' }}>{s.host}</small>
                    {!s.enabled && <small> · {t('Off')}</small>}
                    <span className="bar-track" style={{ display: 'block', marginTop: 4, maxWidth: 220 }}>
                      <span className="bar-fill" style={{ width: `${(s.bytes * 100) / most}%` }} />
                    </span>
                  </td>
                  <td className="mono" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>{formatBytes(s.bytes)}</td>
                  <td className="mono" style={{ textAlign: 'right' }}>{s.articles.toLocaleString(locale())}</td>
                  <td className="mono" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>{asked > 0 ? `${s.missing.toLocaleString(locale())} (${((s.missing * 100) / asked).toFixed(1)}%)` : '–'}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <p className="field-hint">{t('What each server delivered to the built-in downloader, and the articles it did not have, which the next server was then asked for. Downloads that SABnzbd or NZBGet make are not counted here.')}</p>
    </section>
  )
}

// The same traffic month by month, for a provider that is paid by the year:
// which months used how much, and the total over the whole range (#167).
function UsenetMonthsStats({ servers }: { servers: ServerTraffic[] }) {
  const [months, setMonths] = useState<UsenetMonth[] | null>(null)
  const [count, setCount] = useState(12)
  useEffect(() => {
    api.usenetMonths(count).then((r) => setMonths(r.months)).catch(() => undefined)
  }, [count])
  if (!months) return null
  const ids = servers.map((s) => s.id)
  const cell = (m: UsenetMonth, id: number) => m.servers.find((x) => x.id === id)?.bytes ?? 0
  const total = (id: number) => months.reduce((sum, m) => sum + cell(m, id), 0)
  const label = (key: string) => new Date(`${key}-01T12:00:00Z`).toLocaleDateString(locale(), { month: 'long', year: 'numeric', timeZone: 'UTC' })
  return (
    <section className="card" style={{ marginTop: 16 }}>
      <h2 style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
        <TipLabel help="downloads#how-much-each-provider-delivered">{t('Usenet providers, month by month')}</TipLabel>
        <select className="input" value={count} onChange={(e) => setCount(Number(e.target.value))} aria-label={t('Months shown')} style={{ width: 'auto' }}>
          {[6, 12, 24].map((n) => <option key={n} value={n}>{t('Last {n} months', { n })}</option>)}
        </select>
      </h2>
      <div style={{ overflowX: 'auto' }}>
        <table className="indexer-stats" style={{ width: '100%' }}>
          <thead>
            <tr>
              <th style={{ textAlign: 'left' }}>{t('Month')}</th>
              {servers.map((s) => <th key={s.id} style={{ textAlign: 'right' }}>{s.name}</th>)}
              {servers.length > 1 && <th style={{ textAlign: 'right' }}>{t('Total')}</th>}
            </tr>
          </thead>
          <tbody>
            {months.map((m) => (
              <tr key={m.month}>
                <td style={{ whiteSpace: 'nowrap' }}>{label(m.month)}</td>
                {ids.map((id) => <td key={id} className="mono" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>{cell(m, id) > 0 ? formatBytes(cell(m, id)) : '–'}</td>)}
                {servers.length > 1 && <td className="mono" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>{formatBytes(m.servers.reduce((a, x) => a + x.bytes, 0))}</td>}
              </tr>
            ))}
            <tr>
              <td><strong>{t('Total')}</strong></td>
              {ids.map((id) => <td key={id} className="mono" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}><strong>{formatBytes(total(id))}</strong></td>)}
              {servers.length > 1 && <td className="mono" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}><strong>{formatBytes(ids.reduce((a, id) => a + total(id), 0))}</strong></td>}
            </tr>
          </tbody>
        </table>
      </div>
      <p className="field-hint">{t('Months are counted in UTC and are kept for about two years. Counting started with version 4.0, so earlier months are empty.')}</p>
    </section>
  )
}

function titleLink(item: WatchedStatTitle): string {
  return item.kind === 'series' ? `/series/${item.id}` : `/title/${item.tmdbId ?? 0}`
}

// What gets watched, read from Plex, Jellyfin and Emby (Settings > Media
// servers > "Read what's been watched").
function WatchedStats({ w }: { w: WatchStats }) {
  const pct = (a: number, b: number) => (a + b > 0 ? Math.round((a * 100) / (a + b)) : 0)
  const topMax = Math.max(1, ...w.top.map((item) => item.plays))
  return (
    <>
      <h2 style={{ marginTop: 28 }}>{t('What gets watched')}</h2>
      <div className="stat-cards">
        <div className="card stat-card">
          <small>{t('Movies watched')}</small>
          <strong>{w.moviesWatched}</strong>
          <span>
            {t('{pct}% of the {total} you have', { pct: pct(w.moviesWatched, w.moviesUnwatched), total: w.moviesWatched + w.moviesUnwatched })}
          </span>
        </div>
        <div className="card stat-card">
          <small>{t('Episodes watched')}</small>
          <strong>{w.episodesWatched}</strong>
          <span>
            {t('{pct}% of the {total} you have', { pct: pct(w.episodesWatched, w.episodesUnwatched), total: w.episodesWatched + w.episodesUnwatched })}
          </span>
        </div>
        <div className="card stat-card">
          <small>{t('Never watched')}</small>
          <strong>{formatBytes(w.unwatchedBytes)}</strong>
          <span>
            {t('{movies} and {episodes} nobody has played', {
              movies: plural(w.moviesUnwatched, '{n} movie', '{n} movies'),
              episodes: plural(w.episodesUnwatched, '{n} episode', '{n} episodes'),
            })}
          </span>
        </div>
        <div className="card stat-card">
          <small>{t('Plays')}</small>
          <strong>{w.plays}</strong>
          <span>{w.lastSync ? t('Checked {when}', { when: timeAgo(w.lastSync) }) : t('Not checked yet')}</span>
        </div>
      </div>
      <div className="half-cols">
        <section className="card">
          <h2>{t('Most watched')}</h2>
          {w.top.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('Nothing watched yet.')}</p>}
          <div className="bar-list">
            {w.top.map((item) => (
              <div key={`${item.kind}-${item.id}`} className="bar-row">
                <Link className="bar-label" to={titleLink(item)} title={item.title}>
                  {item.title}
                </Link>
                <span className="bar-track">
                  <span className="bar-fill" style={{ width: `${(item.plays * 100) / topMax}%` }} />
                </span>
                <span className="bar-value">
                  {plural(item.plays, '{n} play', '{n} plays')}
                  {item.kind === 'series' ? ` · ${plural(item.episodes ?? 0, '{n} episode', '{n} episodes')}` : ''}
                </span>
              </div>
            ))}
          </div>
        </section>
        <section className="card">
          <h2>{t('Watched lately')}</h2>
          {w.recent.length === 0 && (
            <p style={{ color: 'var(--text-dim)' }}>{w.top.length === 0 ? t('Nothing watched yet.') : t("Your media servers haven't said when things were played.")}</p>
          )}
          <div className="bar-list">
            {w.recent.map((item) => (
              <div key={`${item.kind}-${item.id}`} className="bar-row recent-row">
                <Link className="bar-label" to={titleLink(item)} title={item.title}>
                  {item.title}
                </Link>
                <span className="bar-value">{timeAgo(item.lastPlayed)}</span>
              </div>
            ))}
          </div>
        </section>
      </div>
    </>
  )
}
