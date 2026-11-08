import { useCallback, useEffect, useState } from 'react'
import { api, isAdmin, type RequestQuota, type TitleRequest } from '../api'
import { useAuth } from '../AuthContext'
import { useLive } from '../useLive'
import { locale, plural, t, tx } from '../i18n'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { PosterFallback } from './PosterCard'
import { useToast } from './Toast'
import './RequestPortal.css'

// Activity > Requests: titles basic accounts asked for. Administrators see
// everyone's and approve or decline them; anyone else sees their own and can
// take back one that is still waiting.

const KIND_LABEL: Record<TitleRequest['kind'], string> = { movie: t('Movie'), tv: t('TV show'), music: t('Artist'), book: t('Book') }
const KIND_ICON: Record<TitleRequest['kind'], 'film' | 'tv' | 'music' | 'book'> = { movie: 'film', tv: 'tv', music: 'music', book: 'book' }

function day(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' })
}

// "in about 3 hours" for a moment ahead.
function timeAgoFuture(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now()
  if (Number.isNaN(ms) || ms <= 0) return t('now')
  const h = Math.round(ms / 3600000)
  if (h < 2) return t('in about an hour')
  return h < 36 ? plural(h, 'in about {n} hour', 'in about {n} hours') : plural(Math.round(h / 24), 'in about {n} day', 'in about {n} days')
}

export default function RequestsList({ onCount }: { onCount?: (pending: number) => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const admin = isAdmin(useAuth().user)
  const [list, setList] = useState<TitleRequest[] | null>(null)
  const [busy, setBusy] = useState(0)
  const [declining, setDeclining] = useState<TitleRequest | null>(null)
  const [note, setNote] = useState('')
  const [quota, setQuota] = useState<RequestQuota | null>(null)

  const load = useCallback(() => {
    api
      .listRequests()
      .then((r) => {
        setList(r.requests)
        onCount?.(r.pending)
      })
      .catch(() => setList((cur) => cur ?? []))
    if (!admin) {
      api
        .requestStatus()
        .then((s) => setQuota(s.quota))
        .catch(() => undefined)
    }
  }, [onCount, admin])
  useEffect(load, [load])
  useLive(load, 30000)

  async function act(r: TitleRequest, fn: () => Promise<unknown>, done: string) {
    setBusy(r.id)
    try {
      await fn()
      toast.success(done)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(0)
    }
  }

  // Approving a removal asks first, with the same choice about the files as the Remove button.
  async function approve(r: TitleRequest) {
    if (r.action !== 'remove') {
      await act(r, () => api.approveRequest(r.id), t('{title} approved and added.', { title: r.title }))
      return
    }
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: r.title }),
      body: <p>{t('Mediarium stops tracking it and cancels anything still downloading for it. You can add it back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: {
        label: t('Also delete its files from the disk'),
        hint: t('This deletes their folders in your library too. It cannot be undone.'),
        defaultChecked: false,
        warning: r.kind === 'movie' ? t('The files of this movie will be deleted for good.') : t('The files of this show will be deleted for good.'),
      },
    })
    if (!answer) return
    await act(r, () => api.approveRequest(r.id, answer.checked), t('Removed {title}.', { title: r.title }))
  }

  if (list === null) return <div className="skeleton" style={{ height: 120 }} />
  const quotaLine =
    quota && quota.limit > 0 ? (
      <p className={`request-quota${quota.left === 0 ? ' out' : ''}`}>
        {tx('{left} of {limit} requests left {when}.', {
          left: <strong>{quota.left}</strong>,
          limit: quota.limit,
          when: quota.period === 'day' ? t('today') : t('this week'),
        })}
        {quota.left === 0 && quota.resetsAt ? ` ${t('You can ask again {when}.', { when: timeAgoFuture(quota.resetsAt) })}` : ''}
      </p>
    ) : null
  if (list.length === 0)
    return (
      <>
        {quotaLine}
        <div className="empty-state">
          <Icon name="list" size={30} />
          <p>{admin ? t('No requests. When an account that has to ask adds something, it shows up here for you to approve.') : t('You haven’t asked for anything yet.')}</p>
        </div>
      </>
    )
  return (
    <>
    {quotaLine}
    <div className="request-list">
      {list.map((r) => (
        <div key={r.id} className={`request-row st-${r.status}`}>
          <div className="qthumb">{r.poster ? <img src={r.poster} alt="" loading="lazy" /> : <PosterFallback />}</div>
          <div className="wmain">
            <strong>
              <Icon name={KIND_ICON[r.kind]} size={13} /> {r.title}
              {r.year ? ` (${r.year})` : ''}
              {r.fourK && <span className="fourk-tag">4K</span>}
              {r.action === 'remove' && <span className="fourk-tag removal-tag">{t('Removal')}</span>}
            </strong>
            <small>
              {[KIND_LABEL[r.kind], admin && r.requester ? t('asked by {name}', { name: r.requester }) : '', day(r.createdAt)].filter(Boolean).join(' · ')}
              {r.note ? ` · ${r.note}` : ''}
            </small>
            {r.reason && <small>{t('Reason: {reason}', { reason: r.reason })}</small>}
          </div>
          <span className={`state-tag st-${r.status === 'approved' ? 'added' : r.status === 'declined' ? 'failed' : 'pending'}`}>
            {r.status === 'pending' ? t('Waiting') : r.status === 'approved' ? t('Approved') : t('Declined')}
          </span>
          <div className="row-actions">
            {admin && r.status === 'pending' && (
              <>
                <button className="primary btn-sm btn-with-icon" disabled={busy === r.id} onClick={() => void approve(r)}>
                  <Icon name={r.action === 'remove' ? 'trash' : 'check'} size={14} /> {r.action === 'remove' ? t('Remove') : t('Approve')}
                </button>
                <button className="btn-sm" disabled={busy === r.id} onClick={() => setDeclining(r)}>
                  {t('Decline')}
                </button>
              </>
            )}
            {(admin || r.status === 'pending') && (
              <button
                className="icon-btn"
                title={admin ? t('Remove from the list') : t('Take back this request')}
                aria-label={admin ? t('Remove from the list') : t('Take back this request')}
                disabled={busy === r.id}
                onClick={() => void act(r, () => api.deleteRequest(r.id), admin ? t('Removed from the list.') : t('Request taken back.'))}
              >
                <Icon name="trash" size={15} />
              </button>
            )}
          </div>
          {declining?.id === r.id && (
            <div className="request-decline">
              <input value={note} onChange={(e) => setNote(e.target.value)} placeholder={t('Why not? (optional, they will see it)')} maxLength={300} />
              <button
                className="btn-sm danger-ghost"
                onClick={() =>
                  void act(r, () => api.declineRequest(r.id, note), t('Request declined.')).then(() => {
                    setDeclining(null)
                    setNote('')
                  })
                }
              >
                {t('Decline')}
              </button>
              <button className="btn-sm" onClick={() => setDeclining(null)}>
                {t('Cancel')}
              </button>
            </div>
          )}
        </div>
      ))}
    </div>
    </>
  )
}
