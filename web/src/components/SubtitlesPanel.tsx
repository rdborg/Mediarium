import { useCallback, useEffect, useState } from 'react'
import { api, type SubtitleQuota, type SubtitleResult } from '../api'
import { languageName } from '../languages'
import QuotaNote from './QuotaNote'

// Subtitles for one downloaded movie or episode: which of the configured
// languages are already next to the file, plus a manual search that lists
// candidates best-fit first (subtitles timed for the same release group,
// source and resolution as the video score highest).
export default function SubtitlesPanel({ kind, id }: { kind: 'movie' | 'episode'; id: number }) {
  const [languages, setLanguages] = useState<string[]>([])
  const [present, setPresent] = useState<string[]>([])
  const [lang, setLang] = useState('')
  const [results, setResults] = useState<SubtitleResult[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')
  const [quota, setQuota] = useState<SubtitleQuota | null>(null)
  useEffect(() => {
    api.subtitleQuota().then(setQuota).catch(() => undefined)
  }, [status])

  const loadStatus = useCallback(() => {
    const req = kind === 'movie' ? api.movieSubtitleStatus(id) : api.episodeSubtitleStatus(id)
    req
      .then((s) => {
        setLanguages(s.languages)
        setPresent(s.present)
        setLang((cur) => cur || s.languages[0] || 'en')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [kind, id])
  useEffect(loadStatus, [loadStatus])

  async function search(language: string) {
    setLang(language)
    setSearching(true)
    setResults(null)
    setError('')
    setStatus('')
    try {
      setResults(await (kind === 'movie' ? api.searchSubtitles(id, language) : api.searchEpisodeSubtitles(id, language)))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSearching(false)
    }
  }

  async function download(fileId: number) {
    setStatus('Downloading…')
    setError('')
    try {
      const res = await (kind === 'movie' ? api.downloadSubtitle(id, fileId, lang) : api.downloadEpisodeSubtitle(id, fileId, lang))
      setStatus(`Saved to ${res.path}`)
      loadStatus()
    } catch (e) {
      setStatus('')
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div>
      <QuotaNote quota={quota} />
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginBottom: 8 }}>
        {languages.map((l) => (
          <span key={l} className={`badge ${present.includes(l) ? 'downloaded' : 'missing'}`}>
            {languageName(l)} {present.includes(l) ? '✓' : 'missing'}
          </span>
        ))}
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        {languages.map((l) => (
          <button key={l} disabled={searching} onClick={() => search(l)}>
            Search {languageName(l)}
          </button>
        ))}
      </div>
      {error && <p className="error-text">{error}</p>}
      {status && <p style={{ color: 'var(--text-dim)' }}>{status}</p>}
      {searching && <p style={{ color: 'var(--text-dim)' }}>Searching…</p>}
      {results !== null && results.length === 0 && <p style={{ color: 'var(--text-dim)' }}>No {languageName(lang)} subtitles found.</p>}
      {results !== null && results.length > 0 && (
        <table style={{ marginTop: 8 }}>
          <thead>
            <tr>
              <th>Release</th>
              <th>Rating</th>
              <th>Fit</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {results.map((r, i) => (
              <tr key={r.fileId}>
                <td style={{ wordBreak: 'break-all' }}>
                  {r.release || '—'} {i === 0 && <span className="badge downloaded">best match</span>}
                </td>
                <td>{r.rating}</td>
                <td>{Math.round(r.score)}</td>
                <td>
                  <button onClick={() => download(r.fileId)} disabled={status === 'Downloading…'}>
                    Download
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
