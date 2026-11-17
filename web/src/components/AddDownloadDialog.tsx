import { createPortal } from 'react-dom'
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, type Book, type Episode, type MusicAlbum, type MusicArtist, type Movie, type Series } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'
import { useModules } from '../ModulesContext'
import { t } from '../i18n'
import { cleanAddress } from '../manualAddress'
import { albumOption, emptyTarget, matching, offeredFormats, targetFields, targetProblem, type HandFormat, type HandKind } from '../manualTarget'

// The file size the server applies; saying so here saves a long upload.
const MAX_FILE_BYTES = 32 * 1024 * 1024

// A title the box is opened for from its own page: the album or the book (and
// its format) is already chosen, so only the file is asked for.
export type HandFixed = { kind: 'album'; albumId: number; label: string } | { kind: 'book'; bookId: number; format: HandFormat; label: string }

// Add a download by hand: an NZB or torrent file, an address that points to one,
// or a magnet link, for a movie, an episode, an album or a book (one format of
// it) picked from your library.
// A list shows its first row highlighted before anything is chosen, and clicking
// that row fires no change event. This makes the click choose it anyway.
function chooseRow(value: string, current: number, choose: (id: number) => void) {
  const id = Number(value)
  if (id > 0 && id !== current) choose(id)
}

export default function AddDownloadDialog({ onClose, onAdded, fixed }: { onClose: () => void; onAdded: () => void; fixed?: HandFixed }) {
  const toast = useToast()
  const { on } = useModules()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const kinds: HandKind[] = useMemo(() => {
    const k: HandKind[] = ['movie', 'show']
    if (on('music')) k.push('album')
    if (on('ebooks') || on('audiobooks')) k.push('book')
    return k
  }, [on])
  const [kind, setKind] = useState<HandKind>(fixed?.kind ?? 'movie')
  const [movies, setMovies] = useState<Movie[] | null>(null)
  const [series, setSeries] = useState<Series[] | null>(null)
  const [artists, setArtists] = useState<MusicArtist[] | null>(null)
  const [books, setBooks] = useState<Book[] | null>(null)
  const [filter, setFilter] = useState('')
  const [movieId, setMovieId] = useState(0)
  const [seriesId, setSeriesId] = useState(0)
  const [episodes, setEpisodes] = useState<Episode[] | null>(null)
  const [season, setSeason] = useState('')
  const [episode, setEpisode] = useState('0')
  const [artistId, setArtistId] = useState(0)
  const [albums, setAlbums] = useState<MusicAlbum[] | null>(null)
  const [albumId, setAlbumId] = useState(fixed?.kind === 'album' ? fixed.albumId : 0)
  const [bookId, setBookId] = useState(fixed?.kind === 'book' ? fixed.bookId : 0)
  const [format, setFormat] = useState<HandFormat | ''>(fixed?.kind === 'book' ? fixed.format : '')
  const [source, setSource] = useState<'file' | 'address'>('file')
  const [file, setFile] = useState<File | null>(null)
  const [address, setAddress] = useState('')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Each list is loaded when its kind is first shown (all of them when none is fixed).
  useEffect(() => {
    if (fixed) return
    let live = true
    if (kind === 'movie' && movies === null) api.listMovies().then((m) => live && setMovies(m)).catch(() => live && setMovies([]))
    if (kind === 'show' && series === null) api.listSeries().then((s) => live && setSeries(s)).catch(() => live && setSeries([]))
    if (kind === 'album' && artists === null) api.listArtists().then((a) => live && setArtists(a)).catch(() => live && setArtists([]))
    if (kind === 'book' && books === null) api.listBooks().then((b) => live && setBooks(b)).catch(() => live && setBooks([]))
    return () => {
      live = false
    }
  }, [fixed, kind, movies, series, artists, books])

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

  // The albums of the chosen artist.
  useEffect(() => {
    if (!artistId) return
    let live = true
    api.getArtist(artistId).then((d) => live && setAlbums(d.albums ?? [])).catch(() => live && setAlbums([]))
    return () => {
      live = false
    }
  }, [artistId])

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
  function pickArtist(id: number) {
    setArtistId(id)
    setAlbums(null)
    setAlbumId(0)
  }
  function pickBook(id: number) {
    setBookId(id)
    const b = (books ?? []).find((x) => x.id === id)
    const offered = b ? offeredFormats(b, on('ebooks'), on('audiobooks')) : []
    setFormat(offered[0] ?? '')
  }
  function pickKind(k: HandKind) {
    setKind(k)
    setFilter('')
    setError('')
  }

  const shownMovies = matching((movies ?? []).filter((m) => !m.copyOf), filter, (m) => m.title)
  const shownSeries = matching((series ?? []).filter((s) => !s.copyOf), filter, (s) => s.title)
  const shownArtists = matching(artists ?? [], filter, (a) => a.name)
  const shownBooks = matching(books ?? [], filter, (b) => `${b.title} ${b.author}`)
  const chosenBook = (books ?? []).find((b) => b.id === bookId)
  const formatsOffered = chosenBook ? offeredFormats(chosenBook, on('ebooks'), on('audiobooks')) : []

  const target = { ...emptyTarget(kind), movieId, seriesId, season: seasonShown, episode, albumId, bookId, format }

  function problem(): string {
    const why = targetProblem(target)
    if (why) return why
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
    for (const [k, v] of targetFields(target)) form.set(k, v)
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

  const kindName: Record<HandKind, string> = { movie: t('Movie'), show: t('Episode or season'), album: t('Album'), book: t('Book') }
  const nothingFound =
    (kind === 'movie' && movies !== null && shownMovies.length === 0) ||
    (kind === 'show' && series !== null && shownSeries.length === 0) ||
    (kind === 'album' && artists !== null && shownArtists.length === 0) ||
    (kind === 'book' && books !== null && shownBooks.length === 0)

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
          {fixed ? (
            <p style={{ margin: 0 }}>
              <strong>{kindName[fixed.kind]}:</strong> {fixed.label}
              {fixed.kind === 'book' && ` (${fixed.format === 'ebook' ? t('Ebook') : t('Audiobook')})`}
            </p>
          ) : (
            <>
              <div className="seg" role="radiogroup" aria-label={t('What it is for')}>
                {kinds.map((k) => (
                  <button key={k} role="radio" aria-checked={kind === k} className={kind === k ? 'active' : ''} onClick={() => pickKind(k)} disabled={busy}>
                    {kindName[k]}
                  </button>
                ))}
              </div>

              <label>
                {kind === 'show' ? t('Show') : kind === 'album' ? t('Artist') : kindName[kind]}
                <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t('Search your library')} disabled={busy} />
              </label>
              {kind === 'movie' && (
                <select size={6} aria-label={t('Movie')} value={movieId || ''} onChange={(e) => setMovieId(Number(e.target.value))} onClick={(e) => chooseRow(e.currentTarget.value, movieId, setMovieId)} disabled={busy || movies === null}>
                  {shownMovies.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.title}
                      {m.year ? ` (${m.year})` : ''}
                    </option>
                  ))}
                </select>
              )}
              {kind === 'show' && (
                <select size={6} aria-label={t('Show')} value={seriesId || ''} onChange={(e) => pickShow(Number(e.target.value))} onClick={(e) => chooseRow(e.currentTarget.value, seriesId, pickShow)} disabled={busy || series === null}>
                  {shownSeries.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.title}
                      {s.year ? ` (${s.year})` : ''}
                    </option>
                  ))}
                </select>
              )}
              {kind === 'album' && (
                <select size={6} aria-label={t('Artist')} value={artistId || ''} onChange={(e) => pickArtist(Number(e.target.value))} onClick={(e) => chooseRow(e.currentTarget.value, artistId, pickArtist)} disabled={busy || artists === null}>
                  {shownArtists.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
                </select>
              )}
              {kind === 'book' && (
                <select size={6} aria-label={t('Book')} value={bookId || ''} onChange={(e) => pickBook(Number(e.target.value))} onClick={(e) => chooseRow(e.currentTarget.value, bookId, pickBook)} disabled={busy || books === null}>
                  {shownBooks.map((b) => (
                    <option key={b.id} value={b.id}>
                      {b.title}
                      {b.author ? ` – ${b.author}` : ''}
                    </option>
                  ))}
                </select>
              )}
              {nothingFound && <p className="field-hint">{t('Nothing in your library matches. The title has to be added to your library first.')}</p>}

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

              {kind === 'album' && artistId > 0 && (
                <label>
                  {t('Album')}
                  <select size={5} value={albumId || ''} onChange={(e) => setAlbumId(Number(e.target.value))} onClick={(e) => chooseRow(e.currentTarget.value, albumId, setAlbumId)} disabled={busy || albums === null}>
                    {(albums ?? []).map((a) => (
                      <option key={a.id} value={a.id}>
                        {albumOption(a)}
                      </option>
                    ))}
                  </select>
                </label>
              )}

              {kind === 'book' && bookId > 0 && (
                formatsOffered.length > 0 ? (
                  <div className="seg" role="radiogroup" aria-label={t('Format')}>
                    {formatsOffered.map((f) => (
                      <button key={f} role="radio" aria-checked={format === f} className={format === f ? 'active' : ''} onClick={() => setFormat(f)} disabled={busy}>
                        {f === 'ebook' ? t('Ebook') : t('Audiobook')}
                      </button>
                    ))}
                  </div>
                ) : (
                  <p className="field-hint">{t('This book is not kept as an ebook or an audiobook. Turn one of them on for the book first.')}</p>
                )
              )}
            </>
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
