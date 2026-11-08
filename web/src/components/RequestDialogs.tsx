import { createPortal } from 'react-dom'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { api, isRequested, type SeasonInfo } from '../api'
import { st as serverText, t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import Icon from './Icon'
import { useToast } from './Toast'
import './RequestPortal.css'

// The two small forms of the request portal: asking for one episode of a show,
// and asking for a title in the library to be removed. Both send a request
// that an administrator answers under Activity > Requests.

function Shell({ title, busy, onClose, children, foot }: { title: string; busy: boolean; onClose: () => void; children: ReactNode; foot: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal rp-modal" role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 className="rp-nowrap" style={{ margin: 0 }}>
              {title}
            </h2>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body problem-form">{children}</div>
        <div className="modal-foot">{foot}</div>
      </div>
    </div>,
    document.body,
  )
}

// Ask for one episode of a show, whether or not the show is in the library.
export function AskEpisodeDialog({ tmdbId, title, seasons, onClose }: { tmdbId: number; title: string; seasons: SeasonInfo[]; onClose: () => void }) {
  const toast = useToast()
  const list = seasons.filter((s) => s.episodes > 0)
  const first = list.find((s) => s.number > 0) ?? list[0]
  const [season, setSeason] = useState(first?.number ?? 1)
  const [episode, setEpisode] = useState(1)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const count = list.find((s) => s.number === season)?.episodes ?? 1

  async function send() {
    setBusy(true)
    setError('')
    try {
      const res = await api.addSeries(tmdbId, { episode: { season, number: episode } })
      toast.success(isRequested(res) ? serverText(res.message) : t('Sent.'))
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Shell
      title={t('Ask for one episode')}
      busy={busy}
      onClose={onClose}
      foot={
        <>
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary" onClick={() => void send()} disabled={busy || list.length === 0}>
            {busy ? t('Sending…') : t('Send request')}
          </button>
        </>
      }
    >
      <span className="rp-nowrap" style={{ color: 'var(--text-dim)' }}>
        {title}
      </span>
      <div className="rp-row">
        <label>
          <span className="rp-nowrap">{t('Season')}</span>
          <select
            value={season}
            onChange={(e) => {
              setSeason(Number(e.target.value))
              setEpisode(1)
            }}
          >
            {list.map((s) => (
              <option key={s.number} value={s.number}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span className="rp-nowrap">{t('Episode')}</span>
          <select value={episode} onChange={(e) => setEpisode(Number(e.target.value))}>
            {Array.from({ length: count }, (_, i) => i + 1).map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>
      </div>
      {error && <p className="error-text">{error}</p>}
    </Shell>
  )
}

// Ask for a title in the library to be removed, with an optional reason.
export function AskRemovalDialog({ kind, libraryId, title, onClose }: { kind: 'movie' | 'tv'; libraryId: number; title: string; onClose: () => void }) {
  const toast = useToast()
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function send() {
    setBusy(true)
    setError('')
    try {
      const res = await api.requestRemoval(kind, libraryId, reason.trim())
      toast.success(serverText(res.message))
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Shell
      title={t('Ask to remove')}
      busy={busy}
      onClose={onClose}
      foot={
        <>
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary" onClick={() => void send()} disabled={busy}>
            {busy ? t('Sending…') : t('Send request')}
          </button>
        </>
      }
    >
      <span className="rp-nowrap" style={{ color: 'var(--text-dim)' }}>
        {title}
      </span>
      <label>
        <span className="rp-nowrap">{t('Why should it go? (optional)')}</span>
        <textarea value={reason} onChange={(e) => setReason(e.target.value)} maxLength={300} />
      </label>
      {error && <p className="error-text">{error}</p>}
    </Shell>
  )
}
