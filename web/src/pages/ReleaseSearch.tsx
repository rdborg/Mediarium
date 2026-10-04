import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type SearchResult } from '../api'
import Icon from '../components/Icon'
import ReleaseTable from '../components/ReleaseTable'
import { useToast } from '../components/Toast'

// Search every indexer for a release by name, the way you would on the
// indexer's own site, and download one. Mediarium works out the movie or show
// from the release name, adds it to the library if it isn't there yet, and
// files the download as usual.
export default function ReleaseSearch() {
  const toast = useToast()
  const [q, setQ] = useState('')
  const [results, setResults] = useState<SearchResult[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [grabbing, setGrabbing] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [hideBlocked, setHideBlocked] = useState(true)

  async function search() {
    const term = q.trim()
    if (term.length < 2) return
    setSearching(true)
    setError('')
    try {
      setResults(await api.search(term))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSearching(false)
    }
  }

  async function grab(r: SearchResult) {
    setGrabbing(r.downloadUrl)
    try {
      await api.searchGrab({ releaseTitle: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol })
      toast.success(`Added to the downloads: ${r.title}`)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setGrabbing(null)
    }
  }

  const shown = (results ?? []).filter((r) => !hideBlocked || !r.blocklisted)
  return (
    <div>
      <div className="page-header">
        <h1>Search releases</h1>
        <Link className="btn-link" to="/search">
          <Icon name="film" size={15} /> Search movies and shows instead
        </Link>
      </div>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        Type a release name, or part of one, and Mediarium asks all your indexers. Pick one to download: the movie or show it belongs to is added to your library if it isn&apos;t there yet.
      </p>
      <form
        className="toolbar"
        onSubmit={(e) => {
          e.preventDefault()
          void search()
        }}
      >
        <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="For example: Heat 1995 1080p" aria-label="Release name" style={{ flex: '1 1 320px' }} autoFocus />
        <button className="primary btn-with-icon" type="submit" disabled={searching || q.trim().length < 2}>
          <Icon name="search" size={15} /> {searching ? 'Searching…' : 'Search'}
        </button>
      </form>
      {error && <p className="error-text">{error}</p>}
      {results !== null && (
        <>
          <div className="toolbar">
            <span style={{ color: 'var(--text-dim)' }}>
              {results.length === 0 ? 'Nothing found. Try fewer words, or check your indexers in Settings.' : `${shown.length} ${shown.length === 1 ? 'release' : 'releases'}`}
            </span>
            <span className="spacer" />
            {results.some((r) => r.blocklisted) && (
              <label className="inline-field">
                <input type="checkbox" checked={hideBlocked} onChange={(e) => setHideBlocked(e.target.checked)} />
                Hide blocklisted releases
              </label>
            )}
          </div>
          {shown.length > 0 && <ReleaseTable results={shown} grabbing={grabbing} onGrab={(r) => void grab(r)} showPackBadge />}
        </>
      )}
    </div>
  )
}
