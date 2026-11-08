import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { api, isAdmin, isRequested, isViewer, type IssueCategory, type RequestStatus, type SeasonInfo } from '../api'
import { useAuth } from '../AuthContext'
import { st as serverText, t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { ISSUE_CATEGORY } from './issueCategories'
import Icon from './Icon'
import { AskEpisodeDialog, AskRemovalDialog } from './RequestDialogs'
import { useToast } from './Toast'
import './RequestPortal.css'

// On a movie or show that is in the library: "Ask for a 4K copy" (for an
// account that has to ask, when an administrator allows it), "Ask for one
// episode" (a show, for an account that has to ask), "Ask to remove" (for
// every account but an administrator) and "Report a problem" (for anyone
// signed in, when an administrator allows it).
export default function TitleRequestActions({
  kind,
  tmdbId,
  libraryId,
  title,
}: {
  kind: 'movie' | 'tv'
  tmdbId: number
  libraryId: number
  title: string
}) {
  const toast = useToast()
  const me = useAuth().user
  const admin = isAdmin(me)
  // A viewer can only look: asking for a copy or reporting a problem both change something.
  const viewer = isViewer(me)
  const [st, setSt] = useState<RequestStatus | null>(null)
  const [reporting, setReporting] = useState(false)
  const [busy, setBusy] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [seasons, setSeasons] = useState<SeasonInfo[] | null>(null)
  // The title already has a second copy in the 4K quality.
  const [hasFourK, setHasFourK] = useState(false)
  useEffect(() => {
    if (viewer) return
    api.requestStatus().then(setSt).catch(() => setSt(null))
  }, [viewer])
  useEffect(() => {
    if (viewer || !st || admin || !st.asksToAdd || st.fourKProfileId <= 0) return
    api
      .titleCopies(kind === 'movie' ? 'movie' : 'series', libraryId)
      .then((c) => setHasFourK(c.items.some((x) => x.profileId === st.fourKProfileId)))
      .catch(() => undefined)
  }, [st, admin, viewer, kind, libraryId])

  if (viewer || !st) return null
  const canAskFourK = !admin && st.asksToAdd && (kind === 'movie' ? st.fourK.movie : st.fourK.tv) && !hasFourK
  const canAskEpisode = !admin && st.asksToAdd && kind === 'tv'
  if (!canAskFourK && !canAskEpisode && admin && !st.reports) return null

  async function askEpisode() {
    setBusy(true)
    try {
      setSeasons((await api.tmdbTVDetail(tmdbId)).seasonList ?? [])
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function askFourK() {
    setBusy(true)
    try {
      const res = kind === 'movie' ? await api.addMovie(tmdbId, { fourK: true }) : await api.addSeries(tmdbId, { fourK: true })
      toast.success(isRequested(res) ? serverText(res.message) : t('Sent.'))
      api.requestStatus().then(setSt).catch(() => undefined)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="title-actions">
        {canAskFourK && (
          <button className="btn-with-icon" disabled={busy} onClick={() => void askFourK()} title={st.fourKLabel ? t('A second copy in {quality}', { quality: st.fourKLabel }) : undefined}>
            <Icon name="disc" size={15} /> {t('Ask for a 4K copy')}
          </button>
        )}
        {canAskEpisode && (
          <button className="btn-with-icon" disabled={busy} onClick={() => void askEpisode()}>
            <Icon name="tv" size={15} /> {t('Ask for one episode')}
          </button>
        )}
        {!admin && (
          <button className="btn-with-icon" onClick={() => setRemoving(true)}>
            <Icon name="trash" size={15} /> {t('Ask to remove')}
          </button>
        )}
        {st.reports && (
          <button className="btn-with-icon" onClick={() => setReporting(true)}>
            <Icon name="bug" size={15} /> {t('Report a problem')}
          </button>
        )}
      </div>
      {seasons && <AskEpisodeDialog tmdbId={tmdbId} title={title} seasons={seasons} onClose={() => setSeasons(null)} />}
      {removing && <AskRemovalDialog kind={kind} libraryId={libraryId} title={title} onClose={() => setRemoving(false)} />}
      {reporting && <ProblemDialog kind={kind} libraryId={libraryId} title={title} onClose={() => setReporting(false)} />}
    </>
  )
}

function ProblemDialog({ kind, libraryId, title, onClose }: { kind: 'movie' | 'tv'; libraryId: number; title: string; onClose: () => void }) {
  const toast = useToast()
  const [category, setCategory] = useState<IssueCategory>('wrong-file')
  const [episode, setEpisode] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  async function send() {
    setBusy(true)
    setError('')
    try {
      await api.createIssue({ kind, itemId: libraryId, category, episode: episode.trim() || undefined, message: message.trim() || undefined })
      toast.success(t('Thanks. An administrator will look at it. You can follow it under Activity > Reports.'))
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={t('Report a problem with {name}', { name: title })}>
        <div className="modal-head">
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Report a problem')}</h2>
            <span style={{ color: 'var(--text-dim)' }}>{title}</span>
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>
        <div className="modal-body problem-form">
          <label>
            {t("What's wrong?")}
            <select value={category} onChange={(e) => setCategory(e.target.value as IssueCategory)}>
              {(Object.keys(ISSUE_CATEGORY) as IssueCategory[]).map((c) => (
                <option key={c} value={c}>
                  {ISSUE_CATEGORY[c]}
                </option>
              ))}
            </select>
          </label>
          {kind === 'tv' && (
            <label>
              {t('Which episode? (optional)')}
              <input value={episode} onChange={(e) => setEpisode(e.target.value)} placeholder="S01E03" maxLength={40} />
            </label>
          )}
          <label>
            {category === 'other' ? t('Tell us what is wrong') : t('More detail (optional)')}
            <textarea value={message} onChange={(e) => setMessage(e.target.value)} maxLength={500} placeholder={t('For example: the voices drop out after an hour.')} />
          </label>
          {error && <p className="error-text">{error}</p>}
        </div>
        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary" onClick={() => void send()} disabled={busy || (category === 'other' && !message.trim())}>
            {busy ? t('Sending…') : t('Send report')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
