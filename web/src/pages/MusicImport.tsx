import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type MusicImportJob, type MusicImportResult } from '../api'
import Icon from '../components/Icon'
import { useModules } from '../ModulesContext'
import { required } from '../validate'
import { FieldError, useValidation } from '../useValidation'

const REASON: Record<string, string> = {
  unmatched: 'Not found',
  failed: 'Problem',
}

function ResultRow({ r }: { r: MusicImportResult }) {
  const found = r.status === 'imported' || r.status === 'already'
  const title = r.album ? `${r.artist} – ${r.album}` : r.artist || r.folder
  return (
    <li className={`mi-row st-${r.status}`}>
      <span className="mi-ico">
        <Icon name={found ? 'check' : r.status === 'failed' ? 'warning' : 'search'} size={15} />
      </span>
      <div className="mi-main">
        {found && r.artistId ? <Link to={`/music/artist/${r.artistId}`}>{title}</Link> : <strong>{title}</strong>}
        <small>
          {r.files > 0 ? `${r.files} file${r.files === 1 ? '' : 's'}` : 'no audio files'}
          {r.quality ? ` · ${r.quality === 'Unknown' ? 'quality unknown' : r.quality}` : ''}
          {r.status === 'already' ? ' · already in your library' : ''}
        </small>
        {!found && r.message && <small className="mi-why">{r.message}</small>}
        {!found && <code className="mi-folder">{r.folder}</code>}
      </div>
      {!found && <span className={`badge ${r.status === 'failed' ? 'failed' : 'missing'}`}>{REASON[r.status] ?? r.status}</span>}
    </li>
  )
}

// Importing a music collection you already have: Mediarium reads the music
// folder as Artist/Album, looks each one up on MusicBrainz, and registers what
// it finds where it is. No file is moved, renamed or changed.
export default function MusicImport() {
  const { on, loaded } = useModules()
  const [root, setRoot] = useState('')
  const [job, setJob] = useState<MusicImportJob | null>(null)
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)
  const v = useValidation({
    root: required(job?.root || root, 'Set your music folder under Settings > Media, then come back and scan.'),
  })

  useEffect(() => {
    api.getSettings().then((s) => setRoot(s.musicPath ?? '')).catch(() => undefined)
  }, [])

  const jobId = job?.id
  const phase = job?.phase
  const running = phase === 'scanning' || phase === 'matching'
  useEffect(() => {
    if (!jobId || !running) return
    const t = setInterval(() => {
      api
        .musicScan(jobId)
        .then(setJob)
        .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    }, 1000)
    return () => clearInterval(t)
  }, [jobId, running])

  async function start() {
    if (!v.attempt()) return
    setStarting(true)
    setError('')
    setJob(null)
    try {
      const { id } = await api.startMusicScan()
      setJob(await api.musicScan(id))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setStarting(false)
    }
  }

  const results = job?.results ?? []
  const added = results.filter((r) => r.status === 'imported')
  const already = results.filter((r) => r.status === 'already')
  const missed = results.filter((r) => r.status === 'unmatched' || r.status === 'failed')
  const pct = job && job.total > 0 ? Math.min(100, Math.round((job.done / job.total) * 100)) : 0

  return (
    <div className="music-import">
      <div className="page-header">
        <h1>Import my music collection</h1>
        <Link className="btn-link" to="/library?kind=music">
          <Icon name="music" size={15} /> Back to Music
        </Link>
      </div>

      {loaded && !on('music') && (
        <div className="notice notice-warn" style={{ marginBottom: 18 }}>
          Music is switched off. <Link to="/settings/modules">Switch it on in Settings &gt; Media types</Link> first.
        </div>
      )}

      <section className="card mi-start">
        <div className="mi-start-text">
          <h2>Find the music you already have</h2>
          <p>
            Mediarium reads your music folder, expecting <code>Artist/Album/tracks</code> (a year in the album folder name helps, like <code>Album (1997)</code>), looks up each artist and album, and adds them <strong>right where they are</strong>. Nothing is moved, renamed or changed, and only the albums in your folder are monitored.
          </p>
          <p className="mi-folder-line">
            <Icon name="folder" size={15} /> Music folder: <code>{job?.root || root || '(not set yet)'}</code>{' '}
            <Link to="/settings/media">Change</Link>
          </p>
          <FieldError v={v} name="root" />
        </div>
        <div className="mi-start-action">
          <button className="primary big btn-with-icon" onClick={() => void start()} disabled={starting || running || !on('music')}>
            <Icon name="search" size={18} /> {starting ? 'Starting…' : running ? 'Scanning…' : job ? 'Scan again' : 'Start scanning'}
          </button>
          <small>Safe to run again. Albums you&apos;ve added are listed as already in your library.</small>
        </div>
      </section>

      {error && <p className="error-text">{error}</p>}

      {job && running && (
        <section className="card mi-progress" aria-live="polite">
          <div className="mi-progress-top">
            <strong>{phase === 'scanning' ? 'Reading your folder…' : 'Looking up your albums…'}</strong>
            <span>{phase === 'matching' && job.total > 0 ? `${job.done} of ${job.total} albums` : ''}</span>
          </div>
          <div className="bar active" role="progressbar" aria-valuenow={phase === 'matching' ? pct : undefined} aria-valuemin={0} aria-valuemax={100}>
            <span style={{ width: `${phase === 'scanning' ? 8 : Math.max(4, pct)}%` }} />
          </div>
          <small className="mi-hint">The music database allows one lookup a second, so a big collection takes a while. The page can stay open in the background.</small>
        </section>
      )}

      {job && phase === 'failed' && <p className="error-text">The scan stopped: {job.error || 'something went wrong'}. Check the music folder and try again.</p>}

      {job && phase === 'done' && (
        <>
          <div className="mi-summary">
            <div className="mi-stat good">
              <b>{job.summary.imported}</b>
              <span>albums added</span>
            </div>
            <div className="mi-stat">
              <b>{job.summary.already}</b>
              <span>already in your library</span>
            </div>
            <div className="mi-stat warn">
              <b>{job.summary.unmatched}</b>
              <span>not matched</span>
            </div>
            <div className="mi-stat bad">
              <b>{job.summary.failed}</b>
              <span>with a problem</span>
            </div>
          </div>

          {results.length === 0 && (
            <div className="empty-state">
              <Icon name="folder" size={44} />
              <p>No albums were found in that folder. Check that it holds folders like <code>Artist/Album</code> with audio files inside.</p>
            </div>
          )}

          {results.length > 0 && (
            <div className="half-cols mi-cols">
              <fieldset className="group mi-col found">
                <legend>
                  <Icon name="check" size={14} /> Added to your library ({added.length + already.length})
                </legend>
                {added.length + already.length === 0 ? (
                  <p className="mi-empty">Nothing matched.</p>
                ) : (
                  <ul className="mi-list">
                    {[...added, ...already].map((r) => (
                      <ResultRow key={r.folder} r={r} />
                    ))}
                  </ul>
                )}
              </fieldset>
              <fieldset className="group mi-col lost">
                <legend>
                  <Icon name="search" size={14} /> Not matched ({missed.length})
                </legend>
                {missed.length === 0 ? (
                  <p className="mi-empty">Every folder was matched.</p>
                ) : (
                  <ul className="mi-list">
                    {missed.map((r) => (
                      <ResultRow key={r.folder} r={r} />
                    ))}
                  </ul>
                )}
              </fieldset>
            </div>
          )}

          {added.length + already.length > 0 && (
            <div className="row-actions" style={{ marginTop: 18 }}>
              <Link className="btn-link primary-look" to="/library?kind=music">
                <Icon name="music" size={16} /> See my music library
              </Link>
            </div>
          )}
        </>
      )}
    </div>
  )
}
