import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  api,
  type ImportActive,
  type ImportBatchDetail,
  type ImportBatchItem,
  type ImportCandidate,
  type ImportItem,
  type ImportJob,
} from '../api'
import Switch from '../components/Switch'
import { useToast } from '../components/Toast'
import { formatBytes } from '../format'
import { afterText, etaText, progressText, setText, totalLine, unitFor } from '../importText'
import { useLive } from '../useLive'
import { firstError, folderPath, required } from '../validate'
import { FieldError, useValidation } from '../useValidation'

type Kind = 'movie' | 'tv'

const LAST_TAB = 'mediarium-library-tab'

function startingKind(asked: string | null): Kind {
  if (asked === 'tv' || asked === 'movie') return asked
  try {
    return localStorage.getItem(LAST_TAB) === 'tv' ? 'tv' : 'movie'
  } catch {
    return 'movie'
  }
}

// Import an existing library: point at a folder that already holds movies or
// TV, review what Mediarium recognised (and fix any wrong match), then
// confirm. Confirming only writes the titles down, at once; the details
// (posters, summaries, episode lists) are filled in by the server in the
// background, so leaving this page is safe and coming back picks up where
// the import is. Files are left exactly where they are.
export default function ImportLibrary() {
  const [params, setParams] = useSearchParams()
  const [kind, setKind] = useState<Kind>(() => startingKind(params.get('kind')))
  const [path, setPath] = useState('')
  const [defaults, setDefaults] = useState<{ movie: string; tv: string }>({ movie: '', tv: '' })
  const [job, setJob] = useState<ImportJob | null>(null)
  const [batchId, setBatchId] = useState<number | null>(() => {
    const n = Number(params.get('batch'))
    return Number.isInteger(n) && n > 0 ? n : null
  })
  const [batch, setBatch] = useState<ImportBatchDetail | null>(null)
  const [active, setActive] = useState<ImportActive | null>(null)
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)
  const [confirming, setConfirming] = useState(false)

  // The choices for this import. Both start off, so an import never
  // downloads anything by itself.
  const [monitor, setMonitor] = useState(false)
  const [monitorMissing, setMonitorMissing] = useState(false)

  // Per-row review state, keyed by item key.
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [chosen, setChosen] = useState<Record<string, number>>({})
  const [candidates, setCandidates] = useState<Record<string, ImportCandidate[]>>({})

  const example = kind === 'movie' ? '/movies' : '/tv'
  const v = useValidation({
    path: firstError(required(path, `Add the folder to scan, for example ${example}.`), folderPath(path, example)),
  })

  const defaultsRef = useRef(defaults)
  defaultsRef.current = defaults
  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        const d = { movie: s.moviesPath, tv: s.tvPath ?? '' }
        setDefaults(d)
        setPath((p) => p || d[kind])
      })
      .catch(() => undefined)
    // Only the first load: later changes of kind set the folder themselves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function chooseKind(next: Kind) {
    setKind(next)
    setPath(defaults[next])
    setParams({ kind: next }, { replace: true })
  }

  // Find what is going on at the server: a scan waiting to be reviewed, or an
  // import that is still running or has finished. This is what lets the
  // person leave and come back.
  const attached = useRef(false)
  const refreshActive = useCallback(() => {
    api
      .importActive()
      .then(setActive)
      .catch(() => undefined)
  }, [])
  useEffect(() => refreshActive(), [refreshActive])
  useLive(refreshActive, 3000)
  useEffect(() => {
    if (!active || attached.current) return
    attached.current = true
    if (batchId !== null) return // the link asked for a particular import
    // A scan of this kind that is running or waiting to be reviewed, else an
    // import of this kind that is still filling in details. A finished import
    // is opened from the banner's link, not every time this page opens.
    const scan = active.jobs.find((j) => j.kind === kind)
    if (scan) {
      setPath(scan.root)
      api
        .getScan(scan.id)
        .then(setJob)
        .catch(() => undefined)
      return
    }
    const running = active.batches.find((x) => x.kind === kind && x.running)
    if (running) setBatchId(running.id)
  }, [active, batchId, kind])

  const jobId = job?.id
  const phase = job?.phase
  const reviewInit = useRef<string | null>(null)

  // Poll while the server is scanning and matching.
  useEffect(() => {
    if (!jobId || !(phase === 'scanning' || phase === 'matching')) return
    const t = setInterval(() => {
      api
        .getScan(jobId)
        .then(setJob)
        .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    }, 700)
    return () => clearInterval(t)
  }, [jobId, phase])

  // When a scan first becomes ready, pre-select the confident matches.
  useEffect(() => {
    if (!job || job.phase !== 'ready' || reviewInit.current === job.id) return
    reviewInit.current = job.id
    const c: Record<string, boolean> = {}
    const pick: Record<string, number> = {}
    for (const item of job.items) {
      c[item.key] = item.match === 'matched' && !item.inLibrary
      if (item.candidates.length > 0) pick[item.key] = item.candidates[0].tmdbId
    }
    setChecked(c)
    setChosen(pick)
    setCandidates({})
  }, [job])

  // Follow the import once it is confirmed.
  const loadBatch = useCallback(() => {
    if (batchId === null) return
    api
      .importBatch(batchId)
      .then(setBatch)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [batchId])
  useEffect(() => loadBatch(), [loadBatch])
  useLive(loadBatch, batch?.running === false ? 60000 : 1500)

  async function start() {
    if (!v.attempt()) return
    setError('')
    setStarting(true)
    setJob(null)
    setBatchId(null)
    setBatch(null)
    reviewInit.current = null
    try {
      const { jobId } = await api.scanLibrary(path.trim(), kind)
      setJob(await api.getScan(jobId))
      refreshActive()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setStarting(false)
    }
  }

  const rowCandidates = useCallback(
    (item: ImportItem) => candidates[item.key] ?? item.candidates,
    [candidates],
  )

  async function searchRow(item: ImportItem, query: string) {
    if (!query.trim()) return
    try {
      const found = await api.tmdbSearch(kind, query.trim())
      setCandidates((prev) => ({ ...prev, [item.key]: found }))
      if (found.length > 0) {
        setChosen((prev) => ({ ...prev, [item.key]: found[0].tmdbId }))
        setChecked((prev) => ({ ...prev, [item.key]: true }))
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function confirmImport() {
    if (!job) return
    setError('')
    setConfirming(true)
    const selections = job.items
      .filter((i) => checked[i.key] && chosen[i.key])
      .map((i) => {
        const pick = rowCandidates(i).find((c) => c.tmdbId === chosen[i.key])
        return { key: i.key, tmdbId: chosen[i.key], title: pick?.title, year: pick?.year }
      })
    try {
      const res = await api.runImport(job.id, selections, { monitor, noUpgrade: !monitor, monitorMissing: kind === 'tv' ? monitorMissing : false })
      setJob(null)
      setBatch(null)
      setBatchId(res.batchId)
      setParams({ kind, batch: String(res.batchId) }, { replace: true })
      refreshActive()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setConfirming(false)
    }
  }

  function another() {
    setBatchId(null)
    setBatch(null)
    setJob(null)
    setError('')
    setParams({ kind }, { replace: true })
  }

  const selectedCount = job ? job.items.filter((i) => checked[i.key] && chosen[i.key]).length : 0
  const scanning = job && (job.phase === 'scanning' || job.phase === 'matching')
  const runningHere = !!active?.batches.some((b) => b.running && b.kind === kind)
  const what = kind === 'movie' ? 'movie' : 'TV show'

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>Import existing library</h1>
      <p style={{ color: 'var(--text-dim)' }}>
        Point Mediarium at a folder of movies or TV shows. It matches the file and folder names to the right titles and lets you review before anything is
        added. Your files stay where they are, and nothing is moved, renamed or deleted.
      </p>

      {batchId === null && (
        <section className="card grid-form" style={{ marginBottom: 24 }}>
          <div className="view-switch">
            <button className={kind === 'movie' ? 'active' : ''} onClick={() => chooseKind('movie')} disabled={!!scanning}>
              Movies
            </button>
            <button className={kind === 'tv' ? 'active' : ''} onClick={() => chooseKind('tv')} disabled={!!scanning}>
              TV shows
            </button>
          </div>
          <label>
            Folder
            <input
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder={example}
              disabled={!!scanning}
              {...v.bind('path', path, setPath)}
            />
            <FieldError v={v} name="path" />
          </label>
          <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: 0 }}>
            The path as the container sees it. Running in Docker? Mount the folder first.
          </p>
          {runningHere && (
            <p style={{ margin: 0 }}>
              A {what} import is already running. It carries on in the background, so you can keep using Mediarium.{' '}
              <Link to={`/import?kind=${kind}&batch=${active?.batches.find((b) => b.running && b.kind === kind)?.id}`}>See how it is going</Link>
            </p>
          )}
          <button className="primary" onClick={start} disabled={starting || !!scanning || runningHere}>
            {starting ? 'Starting…' : 'Scan folder'}
          </button>
        </section>
      )}

      {error && <p className="error-text">{error}</p>}

      {batchId === null && job?.phase === 'failed' && <p className="error-text">{job.error}</p>}

      {batchId === null && job && (job.phase === 'scanning' || job.phase === 'matching') && (
        <p>{job.phase === 'scanning' ? 'Scanning files…' : `Matching titles… ${job.done} / ${job.total}`}</p>
      )}

      {batchId === null && job && job.phase === 'ready' && (
        <section>
          {job.items.length === 0 ? (
            <div className="empty-state">No {kind === 'movie' ? 'movies' : 'episodes'} found in that folder.</div>
          ) : (
            <>
              <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginBottom: 12, flexWrap: 'wrap' }}>
                <strong>
                  Found {job.items.length} {unitFor(kind, job.items.length)}
                </strong>
                <button
                  onClick={() =>
                    setChecked(Object.fromEntries(job.items.map((i) => [i.key, i.match === 'matched' && !i.inLibrary])))
                  }
                >
                  Select confident matches
                </button>
                <button onClick={() => setChecked({})}>Select none</button>
              </div>
              <div className="import-options">
                <Switch
                  checked={monitor}
                  onChange={setMonitor}
                  label={kind === 'tv' ? 'Watch these titles for new episodes and better versions' : 'Watch these titles for better versions'}
                  description={
                    kind === 'tv'
                      ? "Downloads new episodes as they come out and replaces episodes below your quality profile's target."
                      : "Replaces movies below your quality profile's target with a better version."
                  }
                />
                {kind === 'tv' && (
                  <Switch
                    checked={monitorMissing}
                    onChange={setMonitorMissing}
                    label="Also look for missing episodes"
                    description="Searches for and downloads the episodes you don't have, including new ones."
                  />
                )}
                <p className="after">
                  <strong>What happens next:</strong> {afterText(kind, monitor, kind === 'tv' ? monitorMissing : false)}
                </p>
              </div>

              <button className="primary" disabled={selectedCount === 0 || confirming} onClick={confirmImport}>
                {confirming ? 'Adding…' : `Import ${selectedCount} selected`}
              </button>

              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: 32 }}></th>
                    <th>Found on disk</th>
                    <th>Matched title</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {job.items.map((item) => (
                    <ReviewRow
                      key={item.key}
                      item={item}
                      kind={kind}
                      checked={!!checked[item.key]}
                      onCheck={(v) => setChecked((p) => ({ ...p, [item.key]: v }))}
                      options={rowCandidates(item)}
                      chosen={chosen[item.key]}
                      onChoose={(id) => setChosen((p) => ({ ...p, [item.key]: id }))}
                      onSearch={(q) => searchRow(item, q)}
                    />
                  ))}
                </tbody>
              </table>

            </>
          )}
          {job.skipped.length > 0 && (
            <details style={{ marginTop: 16 }}>
              <summary>
                {job.skipped.length} {job.skipped.length === 1 ? "file wasn't" : "files weren't"} recognised
              </summary>
              <ul style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-dim)' }}>
                {job.skipped.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ul>
            </details>
          )}
        </section>
      )}

      {batchId !== null && batch && <Report batch={batch} onAnother={another} onChanged={loadBatch} />}
      {batchId !== null && !batch && !error && <p>Loading…</p>}
    </div>
  )
}

// The progress of an import that has been confirmed, and its report.
function Report({ batch, onAnother, onChanged }: { batch: ImportBatchDetail; onAnother: () => void; onChanged: () => void }) {
  const eta = batch.running ? etaText(batch) : ''
  const pct = batch.total > 0 ? (batch.done / batch.total) * 100 : 100
  const episodes = batch.kind === 'tv'
  return (
    <section>
      <h2 style={{ marginTop: 0 }}>{batch.running ? 'Importing your library' : 'Import finished'}</h2>
      {batch.running && (
        <div className="import-progress">
          <div>
            <strong>{progressText(batch)}</strong> {eta} You can leave this page: the import carries on in the background.
          </div>
          <div className="bar active" aria-hidden="true">
            <span style={{ width: `${Math.max(3, pct)}%` }} />
          </div>
        </div>
      )}
      {batch.added > 0 && <WhatWasSet batch={batch} onChanged={onChanged} />}
      <table className="data-table">
        <thead>
          <tr>
            <th>Title</th>
            <th>{episodes ? 'Episodes added' : 'Added'}</th>
            <th>Already in library</th>
            <th>Notes</th>
          </tr>
        </thead>
        <tbody>
          {batch.items.map((it, i) => (
            <ReportRow key={`${it.title}-${i}`} item={it} />
          ))}
        </tbody>
      </table>
      <p className="import-total">{totalLine(batch)}</p>
      <p className="import-note">
        Added means new to your library. Already in library means it was here before, so nothing changed.
        {episodes && ' For shows, the numbers count episodes.'}
        {batch.problems > 0 && ' The ones with problems are retried in the background.'}
      </p>
      <p style={{ display: 'flex', gap: 16, alignItems: 'center', flexWrap: 'wrap' }}>
        <Link to={`/library?kind=${batch.kind}`}>Go to Library →</Link>
        <button onClick={onAnother}>Import another folder</button>
      </p>
    </section>
  )
}

// Says what was set on the imported titles, and offers to start monitoring
// exactly these titles (not the rest of the library) once the import is done.
function WhatWasSet({ batch, onChanged }: { batch: ImportBatchDetail; onChanged: () => void }) {
  const toast = useToast()
  const [busy, setBusy] = useState<'watch' | 'missing' | null>(null)
  const [error, setError] = useState('')
  const shows = batch.kind === 'tv'

  async function run(what: 'watch' | 'missing') {
    setBusy(what)
    setError('')
    try {
      const res = await api.watchImport(batch.id, what === 'watch' ? { watch: true } : { missing: true })
      toast.success(res.message)
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const canWatch = !batch.monitor
  const canMissing = shows && !batch.monitorMissing
  return (
    <div className="import-set">
      <p>
        <strong>What was set:</strong> {setText(batch)}
        {batch.running && (canWatch || canMissing) && ' You can change this when the import has finished.'}
      </p>
      {!batch.running && (canWatch || canMissing) && (
        <div className="import-set-actions">
          {canWatch && (
            <button className="primary" disabled={busy !== null} onClick={() => void run('watch')}>
              {busy === 'watch' ? 'Working…' : 'Start monitoring these titles'}
            </button>
          )}
          {canMissing && (
            <button disabled={busy !== null} onClick={() => void run('missing')}>
              {busy === 'missing' ? 'Working…' : 'Look for missing episodes'}
            </button>
          )}
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}

function ReportRow({ item }: { item: ImportBatchItem }) {
  const waiting = item.state === 'pending'
  let added: string | number
  let already: string | number
  if (item.kind === 'series') {
    added = waiting ? '…' : item.imported
    already = waiting ? '…' : item.skipped
  } else {
    added = item.outcome === 'added' ? 1 : 0
    already = item.outcome === 'already' ? 1 : 0
  }
  const problem = item.state === 'problem' || item.outcome === 'failed'
  return (
    <tr>
      <td>{item.title}</td>
      <td>{added}</td>
      <td>{already}</td>
      <td className={problem ? 'error-text' : undefined} style={problem ? undefined : { color: 'var(--text-dim)' }}>
        {waiting && !item.note ? 'Getting details…' : item.note || (waiting ? 'Getting details…' : '')}
      </td>
    </tr>
  )
}

function ReviewRow(props: {
  item: ImportItem
  kind: Kind
  checked: boolean
  onCheck: (v: boolean) => void
  options: ImportCandidate[]
  chosen: number | undefined
  onChoose: (id: number) => void
  onSearch: (q: string) => void
}) {
  const { item, kind, checked, options, chosen } = props
  const [searching, setSearching] = useState(false)
  const [query, setQuery] = useState(item.title)

  const summary =
    kind === 'tv'
      ? `${item.fileCount} episode file${item.fileCount === 1 ? '' : 's'}${item.seasons && item.seasons.length ? ` · ${seasonRange(item.seasons)}` : ''}`
      : `${item.fileCount > 1 ? `${item.fileCount} files · ` : ''}${item.quality && item.quality !== 'Unknown' ? item.quality + ' · ' : ''}${formatBytes(item.sizeBytes)}`

  return (
    <tr>
      <td>
        <input
          type="checkbox"
          checked={checked}
          disabled={!chosen}
          onChange={(e) => props.onCheck(e.target.checked)}
          aria-label={`Import ${item.title}`}
        />
      </td>
      <td>
        <strong>
          {item.title} {item.year ? `(${item.year})` : ''}
        </strong>
        <div style={{ color: 'var(--text-dim)', fontSize: '0.8rem' }}>{summary}</div>
        <div style={{ color: 'var(--text-dim)', fontSize: '0.75rem', fontFamily: 'var(--font-mono)', wordBreak: 'break-all' }}>
          {item.samplePath}
        </div>
      </td>
      <td>
        {options.length > 0 ? (
          <select value={chosen ?? ''} onChange={(e) => props.onChoose(Number(e.target.value))}>
            {options.map((c) => (
              <option key={c.tmdbId} value={c.tmdbId}>
                {c.title} {c.year ? `(${c.year})` : ''}
              </option>
            ))}
          </select>
        ) : (
          <span style={{ color: 'var(--text-dim)' }}>no match</span>
        )}{' '}
        <button onClick={() => setSearching((v) => !v)}>{searching ? 'Cancel' : 'Search…'}</button>
        {searching && (
          <form
            style={{ display: 'flex', gap: 6, marginTop: 6 }}
            onSubmit={(e) => {
              e.preventDefault()
              props.onSearch(query)
              setSearching(false)
            }}
          >
            <input value={query} onChange={(e) => setQuery(e.target.value)} />
            <button className="primary">Go</button>
          </form>
        )}
        {item.error && <div className="error-text">{item.error}</div>}
      </td>
      <td>
        {item.inLibrary ? (
          <span className="badge downloaded">already in library</span>
        ) : (
          <span className={`badge ${item.match === 'matched' ? 'downloaded' : item.match === 'ambiguous' ? 'conflict' : 'failed'}`}>
            {item.match === 'matched' ? 'matched' : item.match === 'ambiguous' ? 'check match' : 'unmatched'}
          </span>
        )}
      </td>
    </tr>
  )
}

function seasonRange(seasons: number[]): string {
  return seasons.length === 1 ? `Season ${seasons[0]}` : `Seasons ${seasons[0]}–${seasons[seasons.length - 1]}`
}
