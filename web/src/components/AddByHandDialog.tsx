import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'
import { useModules } from '../ModulesContext'
import { t } from '../i18n'
import { checkByHand, type ByHandKind } from '../addByHand'

// Add a book, an audiobook or an album by hand, for when the databases don't
// have it or keep finding the wrong one. It is searched for, downloaded and
// imported like any other, and nothing looked up later changes what is typed here.
export default function AddByHandDialog({ initialKind, initialTitle, onClose }: { initialKind: ByHandKind; initialTitle?: string; onClose: () => void }) {
  const toast = useToast()
  const navigate = useNavigate()
  const { on } = useModules()
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  const booksOn = on('ebooks') || on('audiobooks')
  const musicOn = on('music')
  const [kind, setKind] = useState<ByHandKind>(initialKind === 'music' && !musicOn ? 'book' : initialKind === 'book' && !booksOn ? 'music' : initialKind)
  const [title, setTitle] = useState(initialTitle ?? '')
  const [by, setBy] = useState('') // the author of a book, the artist of an album
  const [year, setYear] = useState('')
  const [albumType, setAlbumType] = useState('album')
  const [externalId, setExternalId] = useState('')
  const [description, setDescription] = useState('')
  const [ebook, setEbook] = useState(on('ebooks'))
  const [audiobook, setAudiobook] = useState(!on('ebooks') && on('audiobooks'))
  const [cover, setCover] = useState<File | null>(null)
  const [searchNow, setSearchNow] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [busy, onClose])

  async function submit() {
    const why = checkByHand({ kind, title, by, year, externalId, ebook, audiobook })
    if (why) {
      setError(why)
      return
    }
    const y = year.trim() ? Number(year.trim()) : 0
    setBusy(true)
    setError('')
    try {
      if (kind === 'book') {
        const b = await api.addBookByHand({ title: title.trim(), author: by.trim(), year: y || undefined, description: description.trim() || undefined, externalId: externalId.trim() || undefined, ebook, audiobook, searchNow })
        if (cover) await api.uploadBookCover(b.id, cover, cover.name).catch(() => toast.error(t('The book was added, but its cover could not be saved. You can add it from the book page.')))
        window.dispatchEvent(new CustomEvent('mediarium:book-added', { detail: { key: b.olKey, id: b.id } }))
        toast.success(t('{title} added.', { title: b.title }))
        onClose()
        navigate(`/book/${b.id}`)
      } else {
        const r = await api.addAlbumByHand({ artist: by.trim(), album: title.trim(), year: y || undefined, type: albumType, externalId: externalId.trim() || undefined, searchNow })
        if (cover) await api.uploadAlbumCover(r.albumId, cover, cover.name).catch(() => toast.error(t('The album was added, but its cover could not be saved.')))
        toast.success(t('{title} added.', { title: r.album.title }))
        onClose()
        navigate(`/music/album/${r.albumId}`)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  const music = kind === 'music'
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal add-by-hand" role="dialog" aria-modal="true" aria-label={t('Add it by hand')}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Add it by hand')}</h2>
            <p style={{ margin: 0, color: 'var(--text-dim)', fontSize: '0.88rem' }}>
              {t('Type in what you are looking for. It is searched for and imported like any other, and nothing looked up later changes it.')}
            </p>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          <div className="seg" role="radiogroup" aria-label={t('What it is')}>
            {booksOn && (
              <button role="radio" aria-checked={kind === 'book'} className={kind === 'book' ? 'active' : ''} onClick={() => setKind('book')} disabled={busy}>
                {t('Book or audiobook')}
              </button>
            )}
            {musicOn && (
              <button role="radio" aria-checked={music} className={music ? 'active' : ''} onClick={() => setKind('music')} disabled={busy}>
                {t('Album')}
              </button>
            )}
          </div>

          <label>
            {music ? t('Album title') : t('Title')}
            <input type="text" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={300} disabled={busy} autoFocus />
          </label>
          <div className="row-fields">
            <label>
              {music ? t('Artist') : t('Author')}
              <input type="text" value={by} onChange={(e) => setBy(e.target.value)} maxLength={300} disabled={busy} />
            </label>
            <label>
              {t('Year (optional)')}
              <input type="text" inputMode="numeric" value={year} onChange={(e) => setYear(e.target.value.replace(/\D/g, '').slice(0, 4))} placeholder="2001" disabled={busy} />
            </label>
          </div>

          {music ? (
            <label>
              {t('Type')}
              <select value={albumType} onChange={(e) => setAlbumType(e.target.value)} disabled={busy}>
                <option value="album">{t('Album')}</option>
                <option value="ep">{t('EP')}</option>
                <option value="single">{t('Single')}</option>
              </select>
            </label>
          ) : (
            <div className="choice-grid" role="group" aria-label={t('Formats')}>
              {on('ebooks') && (
                <button type="button" role="checkbox" aria-checked={ebook} className={`choice-card${ebook ? ' active' : ''}`} onClick={() => setEbook(!ebook)} disabled={busy}>
                  <span className="choice-box" aria-hidden="true">{ebook && <Icon name="check" size={13} />}</span>
                  <strong><Icon name="book" size={15} /> {t('Ebook')}</strong>
                </button>
              )}
              {on('audiobooks') && (
                <button type="button" role="checkbox" aria-checked={audiobook} className={`choice-card${audiobook ? ' active' : ''}`} onClick={() => setAudiobook(!audiobook)} disabled={busy}>
                  <span className="choice-box" aria-hidden="true">{audiobook && <Icon name="check" size={13} />}</span>
                  <strong><Icon name="headphones" size={15} /> {t('Audiobook')}</strong>
                </button>
              )}
            </div>
          )}

          {!music && (
            <label>
              {t('Description (optional)')}
              <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={3} maxLength={5000} disabled={busy} />
            </label>
          )}
          <label>
            {music ? t('Your own ID (optional)') : t('ISBN or your own ID (optional)')}
            <input type="text" value={externalId} onChange={(e) => setExternalId(e.target.value)} maxLength={64} placeholder={music ? t('A catalogue number, for example') : '9781234567897'} disabled={busy} spellCheck={false} autoComplete="off" />
          </label>
          <label>
            {t('Cover (optional)')}
            <input type="file" accept="image/jpeg,image/png,image/webp" onChange={(e) => setCover(e.target.files?.[0] ?? null)} disabled={busy} />
          </label>
          <label className="check-row">
            <input type="checkbox" checked={searchNow} onChange={(e) => setSearchNow(e.target.checked)} disabled={busy} /> {t('Start searching for it now')}
          </label>
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary btn-with-icon" onClick={() => void submit()} disabled={busy || (!booksOn && !musicOn)}>
            <Icon name="plus" size={16} /> {busy ? t('Adding…') : t('Add')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
