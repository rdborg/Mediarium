import { useCallback, useEffect, useState } from 'react'
import { api, isAdmin, type TitleRequest } from '../api'
import { useAuth } from '../AuthContext'
import { useLive } from '../useLive'
import Icon from './Icon'
import { PosterFallback } from './PosterCard'
import { useToast } from './Toast'

// Activity > Requests: titles basic accounts asked for. Administrators see
// everyone's and approve or decline them; anyone else sees their own and can
// take back one that is still waiting.

const KIND_LABEL: Record<TitleRequest['kind'], string> = { movie: 'Movie', tv: 'TV show', music: 'Artist', book: 'Book' }
const KIND_ICON: Record<TitleRequest['kind'], 'film' | 'tv' | 'music' | 'book'> = { movie: 'film', tv: 'tv', music: 'music', book: 'book' }

function day(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

export default function RequestsList({ onCount }: { onCount?: (pending: number) => void }) {
  const toast = useToast()
  const admin = isAdmin(useAuth().user)
  const [list, setList] = useState<TitleRequest[] | null>(null)
  const [busy, setBusy] = useState(0)
  const [declining, setDeclining] = useState<TitleRequest | null>(null)
  const [note, setNote] = useState('')

  const load = useCallback(() => {
    api
      .listRequests()
      .then((r) => {
        setList(r.requests)
        onCount?.(r.pending)
      })
      .catch(() => setList((cur) => cur ?? []))
  }, [onCount])
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

  if (list === null) return <div className="skeleton" style={{ height: 120 }} />
  if (list.length === 0)
    return (
      <div className="empty-state">
        <Icon name="list" size={30} />
        <p>{admin ? 'No requests. When an account that has to ask adds something, it shows up here for you to approve.' : 'You haven’t asked for anything yet.'}</p>
      </div>
    )
  return (
    <div className="request-list">
      {list.map((r) => (
        <div key={r.id} className={`request-row st-${r.status}`}>
          <div className="qthumb">{r.poster ? <img src={r.poster} alt="" loading="lazy" /> : <PosterFallback />}</div>
          <div className="wmain">
            <strong>
              <Icon name={KIND_ICON[r.kind]} size={13} /> {r.title}
              {r.year ? ` (${r.year})` : ''}
            </strong>
            <small>
              {[KIND_LABEL[r.kind], admin && r.requester ? `asked by ${r.requester}` : '', day(r.createdAt)].filter(Boolean).join(' · ')}
              {r.note ? ` · ${r.note}` : ''}
            </small>
          </div>
          <span className={`state-tag st-${r.status === 'approved' ? 'added' : r.status === 'declined' ? 'failed' : 'pending'}`}>
            {r.status === 'pending' ? 'Waiting' : r.status === 'approved' ? 'Approved' : 'Declined'}
          </span>
          <div className="row-actions">
            {admin && r.status === 'pending' && (
              <>
                <button className="primary btn-sm btn-with-icon" disabled={busy === r.id} onClick={() => void act(r, () => api.approveRequest(r.id), `${r.title} approved and added.`)}>
                  <Icon name="check" size={14} /> Approve
                </button>
                <button className="btn-sm" disabled={busy === r.id} onClick={() => setDeclining(r)}>
                  Decline
                </button>
              </>
            )}
            {(admin || r.status === 'pending') && (
              <button
                className="icon-btn"
                title={admin ? 'Remove from the list' : 'Take back this request'}
                aria-label={admin ? 'Remove from the list' : 'Take back this request'}
                disabled={busy === r.id}
                onClick={() => void act(r, () => api.deleteRequest(r.id), admin ? 'Removed from the list.' : 'Request taken back.')}
              >
                <Icon name="trash" size={15} />
              </button>
            )}
          </div>
          {declining?.id === r.id && (
            <div className="request-decline">
              <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="Why not? (optional, they will see it)" maxLength={300} />
              <button
                className="btn-sm danger-ghost"
                onClick={() =>
                  void act(r, () => api.declineRequest(r.id, note), 'Request declined.').then(() => {
                    setDeclining(null)
                    setNote('')
                  })
                }
              >
                Decline
              </button>
              <button className="btn-sm" onClick={() => setDeclining(null)}>
                Cancel
              </button>
            </div>
          )}
        </div>
      ))}
    </div>
  )
}
