import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type BookFormat, type BookImportState } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'

const STATUS_LABEL: Record<string, string> = { imported: 'Added', already: 'Already there', unmatched: 'Not found', failed: 'Failed' }

// "Import": reads the ebooks or audiobooks folder for books that are already
// there and adds them to the library where they are. Nothing is moved.
export default function BookImport({ format, folder, onDone }: { format: BookFormat; folder: string; onDone: () => void }) {
  const toast = useToast()
  const [st, setSt] = useState<BookImportState | null>(null)
  const [starting, setStarting] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const running = st?.phase === 'running'

  const poll = useCallback(() => api.bookImportStatus().then(setSt).catch(() => undefined), [])
  useEffect(() => {
    void poll()
  }, [poll])
  useEffect(() => {
    if (!running) return
    const t = setInterval(() => {
      void api.bookImportStatus().then((s) => {
        setSt(s)
        if (s.phase !== 'running') onDone()
      })
    }, 1500)
    return () => clearInterval(t)
  }, [running, onDone])

  async function start() {
    setStarting(true)
    try {
      setSt(await api.startBookImport(format))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setStarting(false)
    }
  }

  const word = format === 'ebook' ? 'ebooks' : 'audiobooks'
  const results = (st?.results ?? []).filter((r) => r.kind === format)
  const problems = results.filter((r) => r.status === 'unmatched' || r.status === 'failed')
  const listed = showAll ? results : problems
  const sum = st?.summary ?? {}
  return (
    <section className="card book-import">
      <div className="book-import-head">
        <div>
          <h2>Import the {word} you already have</h2>
          <p className="hint">
            Mediarium reads <code>{folder || `your ${word} folder`}</code>, looks each book up on Open Library and adds it to your library where it is. Nothing is moved or renamed. Folders named
            Author/Title work best.
          </p>
        </div>
        <button className="primary btn-with-icon" onClick={() => void start()} disabled={starting || running}>
          <Icon name="folder" size={16} /> {running ? 'Importing…' : st?.phase ? 'Import again' : 'Start import'}
        </button>
      </div>
      {running && (
        <div>
          <div className="bar" title={`${st.done} of ${st.total}`}>
            <span style={{ width: `${st.total ? (st.done / st.total) * 100 : 0}%` }} />
          </div>
          <small className="hint">
            {st.done} of {st.total} looked up…
          </small>
        </div>
      )}
      {st?.phase === 'failed' && <p className="error-text">{st.error}</p>}
      {st?.phase === 'done' && (
        <p>
          {results.length === 0 ? (
            `No ${word} found in the folder.`
          ) : (
            <>
              <strong>{sum.imported ?? 0}</strong> added, {sum.already ?? 0} already in your library, {sum.unmatched ?? 0} not found{sum.failed ? `, ${sum.failed} failed` : ''}.
            </>
          )}
        </p>
      )}
      {results.length > 0 && (
        <>
          <div className="row-actions">
            <button className="btn-sm" onClick={() => setShowAll((v) => !v)}>
              {showAll ? 'Show only the ones not found' : `Show all ${results.length}`}
            </button>
          </div>
          {listed.length > 0 && (
            <div className="table-scroll">
              <table className="import-table">
                <thead>
                  <tr>
                    <th>On disk</th>
                    <th>Read as</th>
                    <th>Result</th>
                  </tr>
                </thead>
                <tbody>
                  {listed.map((r) => (
                    <tr key={`${r.kind}-${r.path}`}>
                      <td>
                        <code>{r.path}</code>
                      </td>
                      <td>
                        {r.title}
                        {r.author ? ` · ${r.author}` : ''}
                      </td>
                      <td title={r.message}>
                        {r.bookId ? <Link to={`/book/${r.bookId}`}>{STATUS_LABEL[r.status] ?? r.status}</Link> : (STATUS_LABEL[r.status] ?? r.status)}
                        {r.message && r.status !== 'imported' ? <small className="hint"> {r.message}</small> : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </section>
  )
}
