import { useCallback, useEffect, useState } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { api, isAdmin, type ImportActive, type ImportBatch } from '../api'
import { useAuth } from '../AuthContext'
import { etaText, finishedText, progressText, upNextText } from '../importText'
import { useLive } from '../useLive'
import Icon from './Icon'

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
  useLive(load, 3000)

  if (!active) return null
  // Scans that are still reading the folder. Once a scan is waiting to be
  // reviewed it is the person's move, not background work.
  const scans = active.jobs.filter((j) => j.phase === 'scanning' || j.phase === 'matching' || j.phase === 'importing')
  const batches = active.batches.filter((b) => !(onImportPage && b.id === shownHere))
  const liveScans = onImportPage ? [] : scans
  if (liveScans.length === 0 && batches.length === 0) return null

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
                ? `Looking through your ${j.kind === 'tv' ? 'TV' : 'movie'} folder.`
                : `Matching your ${j.kind === 'tv' ? 'shows' : 'movies'}: ${j.done} of ${j.total}.`}
            </strong>{' '}
            You can keep using Mediarium.
          </div>
          <Link className="import-banner-link" to={`/import?kind=${j.kind}`}>
            Go to the import
          </Link>
        </div>
      ))}
      {batches.map((b) => (
        <BatchBanner key={b.id} batch={b} onDismissed={load} />
      ))}
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
            <strong>{progressText(b)}</strong> You can keep using Mediarium.
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
          See progress
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
            ? `Mediarium retries the ones with problems in the background. ${upNextText(b, true)}`
            : upNextText(b, true)}
        </small>
      </div>
      <Link className="import-banner-link" to={link}>
        See the report
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
        Dismiss
      </button>
    </div>
  )
}
