import { createPortal } from 'react-dom'
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, type Episode, type Movie, type Series } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'
import { t } from '../i18n'
import { cleanAddress } from '../manualAddress'

// The same limits the server applies; saying so here saves a long upload.
const MAX_FILE_BYTES = 32 * 1024 * 1024

const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim()

// Add a download by hand: an NZB or torrent file, an address that points to one,
// or a magnet link, for a movie or an episode picked from your library.
export default function AddDownloadDialog({ onClose, onAdded }: { onClose: () => void; onAdded: () => void }) {
  const toast = useToast()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const [kind, setKind] = useState<'movie' | 'show'>('movie')
  const [movies, setMovies] = useState<Movie[] | null>(null)
  const [series, setSeries] = useState<Series[] | null>(null)
  const [filter, setFilter] = useState('')
  const [movieId, setMovieId] = useState(0)
  const [seriesId, setSeriesId] = useState(0)
  const [episodes, setEpisodes] = useState<Episode[] | null>(null)
  const [season, setSeason] = useState('')
  const [episode, setEpisode] = useState('0')
  const [source, setSource] = useState<'file' | 'address'>('file')
  const [file, setFile] = useState<File | null>(null)
  const [address, setAddress] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let live = true
    api.listMovies().then((m) => live && setMovies(m)).catch(() => live && setMovies([]))
    api.listSeries().then((s) => live && setSeries(s)).catch(() => live && setSeries([]))
    return () => {
      live = false
    }
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [busy, onClose])

  // The seasons and episodes of the chosen show.
  useEffect(() => {
    if (!seriesId) return
    let live = true
    api.getSeries(seriesId).then((d) => live && setEpisodes(d.episodes)).catch(() => live && setEpisodes([]))
    return () => {
      live = false
    }
  }, [seriesId])

  const seasons = useMemo(() => Array.from(new Set((episodes ?? []).map((e) => e.season))).sort((a, b) => a - b), [episodes])
  // Until a season is chosen, the first real one (not the specials) is shown.
  const seasonShown = season !== '' ? season : seasons.length > 0 ? String(seasons.find((n) => n > 0) ?? seasons[0]) : ''
  const seasonEpisodes = useMemo(() => (episodes ?? []).filter((e) => String(e.season) === seasonShown).sort((a, b) => a.episode - b.episode), [episodes, seasonShown])

  function pickShow(id: number) {
    setSeriesId(id)
    setEpisodes(null)
    setSeason('')
    setEpisode('0')
  }

  const q = norm(filter)
  const shownMovies = (movies ?? []).filter((m) => !m.copyOf && (!q || norm(m.title).includes(q))).slice(0, 200)
  const shownSeries = (series ?? []).filter((s) => !s.copyOf && (!q || norm(s.title).includes(q))).slice(0, 200)

  function problem(): string {
    if (kind === 'movie' && !movieId) return t('Choose the movie this download is for.')
    if (kind === 'show' && !seriesId) return t('Choose the show this download is for.')
    if (kind === 'show' && seasonShown === '') return t('Choose the season.')
    if (source === 'file') {
      if (!file) return t('Choose an NZB or .torrent file.')
      if (file.size === 0) return t('That file is empty.')
      if (file.size > MAX_FILE_BYTES) return t('That file is too large. NZB files can be up to 32 MB.')
    } else {
      const a = cleanAddress(address)
      if (!a) return t('Paste an address or a magnet link.')
      if (!/^(https?:\/\/\S+|magnet:\?\S+)$/i.test(a)) return t('Paste a web address that starts with http:// or https://, or a magnet link.')
    }
    return ''
  }

  async function submit() {
    const why = problem()
    if (why) {
      setError(why)
      return
    }
    const form = new FormData()
    if (kind === 'movie') {
      form.set('movieId', String(movieId))
    } else {
      form.set('seriesId', String(seriesId))
      form.set('season', seasonShown)
      form.set('episode', episode)
    }
    if (source === 'file' && file) form.set('file', file)
    else form.set('url', cleanAddress(address))
    if (name.trim()) form.set('name', name.trim())
    setBusy(true)
    setError('')
    try {
      const r = await api.manualDownload(form)
      toast.success(t('Added "{name}" to the download line.', { name: r.releaseTitle }))
      onAdded()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal add-download" role="dialog" aria-modal="true" aria-label={t('Add a download by hand')}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Add a download by hand')}</h2>
            <p style={{ margin: 0, color: 'var(--text-dim)', fontSize: '0.88rem' }}>
              {t('Use an NZB or torrent you already have. It goes into the download line for the title you choose, and is checked and imported like any other download.')}
            </p>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          <div className="seg" role="radiogroup" aria-label={t('What it is for')}>
            <button role="radio" aria-checked={kind === 'movie'} className={kind === 'movie' ? 'active' : ''} onClick={() => setKind('movie')} disabled={busy}>
              {t('Movie')}
            </button>
            <button role="radio" aria-checked={kind === 'show'} className={kind === 'show' ? 'active' : ''} onClick={() => setKind('show')} disabled={busy}>
              {t('Episode or season')}
            </button>
          </div>

          <label>
            {kind === 'movie' ? t('Movie') : t('Show')}
            <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t('Search your library')} disabled={busy} />
          </label>
          {kind === 'movie' ? (
            <select size={6} aria-label={t('Movie')} value={movieId || ''} onChange={(e) => setMovieId(Number(e.target.value))} disabled={busy || movies === null}>
              {shownMovies.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.title}
                  {m.year ? ` (${m.year})` : ''}
                </option>
              ))}
            </select>
          ) : (
            <select size={6} aria-label={t('Show')} value={seriesId || ''} onChange={(e) => pickShow(Number(e.target.value))} disabled={busy || series === null}>
              {shownSeries.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.title}
                  {s.year ? ` (${s.year})` : ''}
                </option>
              ))}
            </select>
          )}
          {((kind === 'movie' && movies !== null && shownMovies.length === 0) || (kind === 'show' && series !== null && shownSeries.length === 0)) && (
            <p className="field-hint">{t('Nothing in your library matches. The title has to be added to your library first.')}</p>
          )}

          {kind === 'show' && seriesId > 0 && (
            <div className="row-fields">
              <label>
                {t('Season')}
                <select value={seasonShown} onChange={(e) => { setSeason(e.target.value); setEpisode('0') }} disabled={busy || episodes === null}>
                  {seasons.map((s) => (
                    <option key={s} value={s}>
                      {s === 0 ? t('Specials') : t('Season {n}', { n: s })}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {t('Episode')}
                <select value={episode} onChange={(e) => setEpisode(e.target.value)} disabled={busy || episodes === null}>
                  <option value="0">{t('The whole season')}</option>
                  {seasonEpisodes.map((e) => (
                    <option key={e.id} value={e.episode}>
                      {e.episode}
                      {e.title ? `. ${e.title}` : ''}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          )}

          <div className="seg" role="radiogroup" aria-label={t('Where it comes from')}>
            <button role="radio" aria-checked={source === 'file'} className={source === 'file' ? 'active' : ''} onClick={() => setSource('file')} disabled={busy}>
              {t('A file')}
            </button>
            <button role="radio" aria-checked={source === 'address'} className={source === 'address' ? 'active' : ''} onClick={() => setSource('address')} disabled={busy}>
              {t('An address or magnet link')}
            </button>
          </div>
          {source === 'file' ? (
            <label>
              {t('NZB or torrent file')}
              <input type="file" accept=".nzb,.torrent,application/x-nzb,application/x-bittorrent" onChange={(e) => setFile(e.target.files?.[0] ?? null)} disabled={busy} />
            </label>
          ) : (
            <label>
              {t('Address or magnet link')}
              <input type="text" value={address} onChange={(e) => setAddress(e.target.value)} placeholder={t('https://… or magnet:?…')} disabled={busy} spellCheck={false} autoComplete="off" />
            </label>
          )}
          <label>
            {t('Release name (optional)')}
            <input type="text" value={name} onChange={(e) => setName(e.target.value)} maxLength={200} placeholder={t('Taken from the file or address')} disabled={busy} />
          </label>
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary btn-with-icon" onClick={() => void submit()} disabled={busy}>
            <Icon name="download" size={16} /> {busy ? t('Adding…') : t('Add download')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
