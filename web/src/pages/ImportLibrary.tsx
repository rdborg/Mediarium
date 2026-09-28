import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type ImportCandidate, type ImportItem, type ImportJob } from '../api'
import { formatBytes } from '../format'

type Kind = 'movie' | 'tv'

// Import an existing library: point at a folder that already holds movies or
// TV, review what Mediarium recognised (and fix any wrong match), then
// register it — files are left exactly where they are.
export default function ImportLibrary() {
  const [kind, setKind] = useState<Kind>('movie')
  const [path, setPath] = useState('')
  const [defaults, setDefaults] = useState<{ movie: string; tv: string }>({ movie: '', tv: '' })
  const [job, setJob] = useState<ImportJob | null>(null)
  const [error, setError] = useState('')
  const [starting, setStarting] = useState(false)

  // Per-row review state, keyed by item key.
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [chosen, setChosen] = useState<Record<string, number>>({})
  const [candidates, setCandidates] = useState<Record<string, ImportCandidate[]>>({})

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setDefaults({ movie: s.moviesPath, tv: s.tvPath ?? '' })
        setPath((p) => p || s.moviesPath)
      })
      .catch(() => undefined)
  }, [])

  function chooseKind(next: Kind) {
    setKind(next)
    setPath(defaults[next])
  }

  const jobId = job?.id
  const phase = job?.phase
  const reviewInit = useRef<string | null>(null)

  // Poll while the server is working.
  useEffect(() => {
    if (!jobId || !(phase === 'scanning' || phase === 'matching' || phase === 'importing')) return
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

  async function start() {
    setError('')
    setStarting(true)
    setJob(null)
    reviewInit.current = null
    try {
      const { jobId } = await api.scanLibrary(path.trim(), kind)
      setJob(await api.getScan(jobId))
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

  async function runImport() {
    if (!job) return
    setError('')
    const selections = job.items
      .filter((i) => checked[i.key] && chosen[i.key])
      .map((i) => ({ key: i.key, tmdbId: chosen[i.key] }))
    try {
      await api.runImport(job.id, selections)
      setJob(await api.getScan(job.id))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const selectedCount = job ? job.items.filter((i) => checked[i.key] && chosen[i.key]).length : 0
  const busy = job && (job.phase === 'scanning' || job.phase === 'matching' || job.phase === 'importing')

  return (
    <div>
      <h1 style={{ marginTop: 0 }}>Import existing library</h1>
      <p style={{ color: 'var(--text-dim)' }}>
        Point Mediarium at a folder that already contains movies or TV shows. It reads the file and folder names,
        matches them on TMDB, and lets you review before anything is added. Your files stay where they are — nothing
        is moved, renamed or deleted.
      </p>

      <section className="card grid-form" style={{ marginBottom: 24 }}>
        <div className="view-switch">
          <button className={kind === 'movie' ? 'active' : ''} onClick={() => chooseKind('movie')} disabled={!!busy}>
            Movies
          </button>
          <button className={kind === 'tv' ? 'active' : ''} onClick={() => chooseKind('tv')} disabled={!!busy}>
            TV shows
          </button>
        </div>
        <label>
          Folder
          <input
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder={kind === 'movie' ? '/movies' : '/tv'}
            disabled={!!busy}
          />
        </label>
        <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: 0 }}>
          The path as the container sees it. Running in Docker? Mount the folder first.
        </p>
        <button className="primary" onClick={start} disabled={!path.trim() || starting || !!busy}>
          {starting ? 'Starting…' : 'Scan folder'}
        </button>
      </section>

      {error && <p className="error-text">{error}</p>}

      {job?.phase === 'failed' && <p className="error-text">{job.error}</p>}

      {job && (job.phase === 'scanning' || job.phase === 'matching') && (
        <p>{job.phase === 'scanning' ? 'Scanning files…' : `Matching titles on TMDB… ${job.done} / ${job.total}`}</p>
      )}

      {job && job.phase === 'ready' && (
        <section>
          {job.items.length === 0 ? (
            <div className="empty-state">No {kind === 'movie' ? 'movies' : 'episodes'} found in that folder.</div>
          ) : (
            <>
              <div style={{ display: 'flex', gap: 12, alignItems: 'center', marginBottom: 12, flexWrap: 'wrap' }}>
                <strong>
                  Found {job.items.length} {kind === 'movie' ? 'movies' : 'shows'}
                </strong>
                <button
                  onClick={() =>
                    setChecked(Object.fromEntries(job.items.map((i) => [i.key, i.match === 'matched' && !i.inLibrary])))
                  }
                >
                  Select confident matches
                </button>
                <button onClick={() => setChecked({})}>Select none</button>
                <button className="primary" disabled={selectedCount === 0} onClick={runImport}>
                  Import {selectedCount} selected
                </button>
              </div>
              <table className="data-table">
                <thead>
                  <tr>
                    <th style={{ width: 32 }}></th>
                    <th>Found on disk</th>
                    <th>Match on TMDB</th>
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
              <summary>{job.skipped.length} file(s) not recognised</summary>
              <ul style={{ fontFamily: 'var(--font-mono)', fontSize: '0.8rem', color: 'var(--text-dim)' }}>
                {job.skipped.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ul>
            </details>
          )}
        </section>
      )}

      {job && job.phase === 'importing' && (
        <p>
          Importing… {job.done} / {job.total}
        </p>
      )}

      {job && job.phase === 'done' && (
        <section>
          <h2>Import finished</h2>
          <table className="data-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Added</th>
                <th>Already had</th>
                <th>Notes</th>
              </tr>
            </thead>
            <tbody>
              {job.results.map((r) => (
                <tr key={r.key}>
                  <td>{r.title}</td>
                  <td>{r.imported}</td>
                  <td>{r.skipped}</td>
                  <td className={r.error ? 'error-text' : undefined} style={r.error ? undefined : { color: 'var(--text-dim)' }}>
                    {r.error || r.message || ''}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p>
            <Link to="/library">Go to Library →</Link>
          </p>
        </section>
      )}
    </div>
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
      ? `${item.fileCount} episode file(s)${item.seasons && item.seasons.length ? ` · ${seasonRange(item.seasons)}` : ''}`
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
