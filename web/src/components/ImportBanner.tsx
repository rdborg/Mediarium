import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { api, isAdmin, type ImportActive, type ImportBatch } from '../api'
import { useAuth } from '../AuthContext'
import { etaText, finishedText, progressText, upNextText } from '../importText'
import { useLive } from '../useLive'
import Icon from './Icon'
import { plural, t } from '../i18n'

// A wide banner under the top bar, on every page, while a library import is
// running (or has just finished). An import carries on in the background, so
// this is how you see it and find your way back to it. Only administrators
// can import, so only they see it.
export default function ImportBanner() {
  return isAdmin(useAuth().user) ? <Banners /> : null
}

function Banners() {
  const [active, setActive] = useState<ImportActive | null>(null)
  // The import page shows its own import in full, so the banner leaves that one out there.
  const onImportPage = useLocation().pathname === '/import'
  const shownHere = Number(useSearchParams()[0].get('batch'))
  const load = useCallback(() => {
    api
      .importActive()
      .then(setActive)
      .catch(() => undefined)
  }, [])
  useEffect(() => load(), [load])
  // Every 3 seconds while an import is working, once in a while when none is (it was every 3 seconds on every page, always).
  const working = !!active && (active.jobs.some((j) => j.phase === 'scanning' || j.phase === 'matching' || j.phase === 'importing') || active.batches.some((b) => b.running))
  useLive(load, working ? 3000 : 20000)

  if (!active) return null
  // Scans that are still reading the folder. Once a scan is waiting to be
  // reviewed it is the person's move, not background work.
  const scans = active.jobs.filter((j) => j.phase === 'scanning' || j.phase === 'matching' || j.phase === 'importing')
  const batches = active.batches.filter((b) => !(onImportPage && b.id === shownHere))
  const liveScans = onImportPage ? [] : scans
  if (liveScans.length === 0 && batches.length === 0) return null
  // Running imports all show. Of the finished ones only the newest of each
  // kind does, so a few imports in a row don't stack up over every page.
  const finished = batches.filter((b) => !b.running)
  const shown = batches.filter((b) => b.running || finished.find((f) => f.kind === b.kind) === b)
  const hidden = finished.length - shown.filter((b) => !b.running).length

  return (
    <div className="import-banners">
      {liveScans.map((j) => (
        <div key={j.id} className="import-banner" role="status">
          <span className="import-banner-icon">
            <Icon name="folder" size={22} />
          </span>
          <div className="import-banner-body">
            <strong>
              {j.phase === 'scanning'
                ? j.kind === 'tv'
                  ? t('Looking through your TV folder.')
                  : t('Looking through your movie folder.')
                : j.kind === 'tv'
                  ? t('Matching your shows: {done} of {total}.', { done: j.done, total: j.total })
                  : t('Matching your movies: {done} of {total}.', { done: j.done, total: j.total })}
            </strong>{' '}
            {t('You can keep using Mediarium.')}
          </div>
          <Link className="import-banner-link" to={`/import?kind=${j.kind}`}>
            {t('Go to the import')}
          </Link>
        </div>
      ))}
      {shown.map((b) => (
        <BatchBanner key={b.id} batch={b} onDismissed={load} />
      ))}
      {finished.length > 1 && (
        <div className="import-banners-all">
          {hidden > 0 && <small>{plural(hidden, '{n} earlier import also finished.', '{n} earlier imports also finished.')}</small>}
          <button
            className="btn-sm"
            onClick={() => void Promise.all(finished.map((b) => api.dismissImportBatch(b.id).catch(() => undefined))).then(load)}
          >
            {t('Dismiss all')}
          </button>
        </div>
      )}
    </div>
  )
}

function BatchBanner({ batch: b, onDismissed }: { batch: ImportBatch; onDismissed: () => void }) {
  const link = `/import?kind=${b.kind}&batch=${b.id}`
  if (b.running) {
    const pct = b.total > 0 ? Math.max(3, (b.done / b.total) * 100) : 3
    const eta = etaText(b)
    return (
      <div className="import-banner" role="status">
        <span className="import-banner-icon">
          <Icon name="folder" size={22} />
        </span>
        <div className="import-banner-body">
          <div>
            <strong>{progressText(b)}</strong> {t('You can keep using Mediarium.')}
          </div>
          <div className="bar active import-banner-bar" aria-hidden="true">
            <span style={{ width: `${pct}%` }} />
          </div>
          <small>
            {eta ? `${eta} ` : ''}
            {upNextText(b)}
          </small>
        </div>
        <Link className="import-banner-link" to={link}>
          {t('See progress')}
        </Link>
      </div>
    )
  }
  const problems = b.problems > 0
  return (
    <div className={`import-banner ${problems ? 'warn' : 'done'}`} role="status">
      <span className="import-banner-icon">
        <Icon name={problems ? 'warning' : 'check'} size={22} />
      </span>
      <div className="import-banner-body">
        <div>
          <strong>{finishedText(b)}</strong>
        </div>
        <small>
          {problems
            ? t('Mediarium retries the ones with problems in the background. {next}', { next: upNextText(b, true) })
            : upNextText(b, true)}
        </small>
      </div>
      <Link className="import-banner-link" to={link}>
        {t('See the report')}
      </Link>
      <button
        className="btn-sm"
        onClick={() =>
          void api
            .dismissImportBatch(b.id)
            .catch(() => undefined)
            .then(onDismissed)
        }
      >
        {t('Dismiss')}
      </button>
    </div>
  )
}
