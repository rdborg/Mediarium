import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type MusicImportJob, type MusicImportResult } from '../api'
import Icon from '../components/Icon'
import KindGlyph from '../components/KindGlyph'
import { useModules } from '../ModulesContext'
import { required } from '../validate'
import { FieldError, useValidation } from '../useValidation'
import { plural, t, tx, st } from '../i18n'
import '../import.css'

const REASON: Record<string, string> = {
  unmatched: t('Not found'),
  failed: t('Problem'),
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
          {r.files > 0 ? plural(r.files, '{n} file', '{n} files') : t('no audio files')}
          {r.quality ? ` · ${r.quality === 'Unknown' ? t('quality unknown') : r.quality}` : ''}
          {r.status === 'already' ? ` · ${t('already in your library')}` : ''}
        </small>
        {!found && r.message && <small className="mi-why">{st(r.message)}</small>}
        {r.note && <small className="mi-why">{st(r.note)}</small>}
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
  const [stopping, setStopping] = useState(false)
  const v = useValidation({
    root: required(job?.root || root, t('Set your music folder under Settings > Library and files > Folders, then come back and scan.')),
  })

  useEffect(() => {
    api.getSettings().then((s) => setRoot(s.musicPath ?? '')).catch(() => undefined)
    // A scan keeps running when the page is left: show it again.
    api
      .latestMusicScan()
      .then((j) => {
        if (j.id) setJob((cur) => cur ?? (j as MusicImportJob))
      })
      .catch(() => undefined)
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

  async function stop() {
    if (!jobId) return
    setStopping(true)
    try {
      setJob(await api.stopMusicScan(jobId))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setStopping(false)
    }
  }

  const results = job?.results ?? []
  const added = results.filter((r) => r.status === 'imported')
  const already = results.filter((r) => r.status === 'already')
  const missed = results.filter((r) => r.status === 'unmatched' || r.status === 'failed')
  const pct = job && job.total > 0 ? Math.min(100, Math.round((job.done / job.total) * 100)) : 0

  return (
    <div className="music-import import-page">
      <div className="page-header">
        <h1>{t('Import my music collection')}</h1>
        <Link className="btn-link" to="/library?kind=music">
          <Icon name="music" size={15} /> {t('Back to Music')}
        </Link>
      </div>

      {loaded && !on('music') && (
        <div className="notice notice-warn" style={{ marginBottom: 18 }}>
          {tx('Music is switched off. {link} first.', { link: <Link to="/settings/modules">{t('Switch it on in Settings > Basics > Media types')}</Link> })}
        </div>
      )}

      <section className="card mi-start">
        <span className={`mi-disc${running ? ' spinning' : ''}`} aria-hidden="true">
          <KindGlyph kind="music" size={72} variant="solid" />
        </span>
        <div className="mi-start-text">
          <h2>{t('Find the music you already have')}</h2>
          <p>
            {tx(
              'Mediarium reads your music folder, expecting {layout} (a year in the album folder name helps, like {example}), looks up each artist and album, and adds them {here}. Nothing is moved, renamed or changed, and only the albums in your folder are monitored.',
              { layout: <code>Artist/Album/tracks</code>, example: <code>Album (1997)</code>, here: <strong>{t('right where they are')}</strong> },
            )}
          </p>
          <p className="mi-folder-line">
            <Icon name="folder" size={15} /> {tx('Music folder: {path}', { path: <code>{job?.root || root || t('(not set yet)')}</code> })}{' '}
            <Link to="/settings/media">{t('Change')}</Link>
          </p>
          <FieldError v={v} name="root" />
        </div>
        <div className="mi-start-action">
          <button className="primary big btn-with-icon" onClick={() => void start()} disabled={starting || running || !on('music')}>
            <Icon name="search" size={18} /> {starting ? t('Starting…') : running ? t('Scanning…') : job ? t('Scan again') : t('Start scanning')}
          </button>
          <small>{t("Safe to run again. Albums you've added are listed as already in your library.")}</small>
        </div>
      </section>

      {error && <p className="error-text">{error}</p>}

      {job && running && (
        <section className="card mi-progress" aria-live="polite">
          <div className="mi-progress-top">
            <strong>{phase === 'scanning' ? t('Reading your folder…') : t('Looking up your albums…')}</strong>
            <span>{phase === 'matching' && job.total > 0 ? plural(job.total, '{done} of {n} album', '{done} of {n} albums', { done: job.done }) : ''}</span>
          </div>
          <div className="bar active" role="progressbar" aria-valuenow={phase === 'matching' ? pct : undefined} aria-valuemin={0} aria-valuemax={100}>
            <span style={{ width: `${phase === 'scanning' ? 8 : Math.max(4, pct)}%` }} />
          </div>
          {job.current && (
            <small className="mi-hint">{tx('Now: {folder}', { folder: <code>{job.current}</code> })}</small>
          )}
          <small className="mi-hint">{t('The music database allows one lookup a second, so a big collection takes a while. You can leave this page, the scan keeps going.')}</small>
          <div className="row-actions">
            <button className="btn-with-icon" onClick={() => void stop()} disabled={stopping}>
              <Icon name="stop" size={15} /> {stopping ? t('Stopping…') : t('Stop the scan')}
            </button>
          </div>
        </section>
      )}

      {job && phase === 'stopped' && (
        <p className="notice">{t('You stopped the scan. The albums it had already added stay in your library, and the list below shows them. Scan again to pick up the rest.')}</p>
      )}

      {job && phase === 'failed' && <p className="error-text">{t('The scan stopped: {error}. Check the music folder and try again.', { error: job.error || t('something went wrong') })}</p>}

      {job && (phase === 'done' || phase === 'stopped') && (
        <>
          <div className="mi-summary">
            <div className="mi-stat good">
              <b>{job.summary.imported}</b>
              <span>{t('albums added')}</span>
            </div>
            <div className="mi-stat">
              <b>{job.summary.already}</b>
              <span>{t('already in your library')}</span>
            </div>
            <div className="mi-stat warn">
              <b>{job.summary.unmatched}</b>
              <span>{t('not matched')}</span>
            </div>
            <div className="mi-stat bad">
              <b>{job.summary.failed}</b>
              <span>{t('with a problem')}</span>
            </div>
          </div>

          {results.length === 0 && phase === 'done' && (
            <div className="empty-state">
              <Icon name="folder" size={44} />
              <p>{tx('No albums were found in that folder. Check that it holds folders like {example} with audio files inside.', { example: <code>Artist/Album</code> })}</p>
            </div>
          )}

          {results.length > 0 && (
            <div className="half-cols mi-cols">
              <fieldset className="group mi-col found">
                <legend>
                  <Icon name="check" size={14} /> {t('Added to your library ({n})', { n: added.length + already.length })}
                </legend>
                {added.length + already.length === 0 ? (
                  <p className="mi-empty">{t('Nothing matched.')}</p>
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
                  <Icon name="search" size={14} /> {t('Not matched ({n})', { n: missed.length })}
                </legend>
                {missed.length === 0 ? (
                  <p className="mi-empty">{t('Every folder was matched.')}</p>
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
                <Icon name="music" size={16} /> {t('See my music library')}
              </Link>
            </div>
          )}
        </>
      )}
    </div>
  )
}
