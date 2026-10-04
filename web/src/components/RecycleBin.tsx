import { useCallback, useEffect, useState } from 'react'
import { api, type TrashList } from '../api'
import { formatBytes, timeAgo } from '../format'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import { useToast } from './Toast'

// Activity > Recycle bin: files of titles removed "with their files", kept a
// few days so a slip can be undone. Putting files back does not add the
// title to the library again; Import (or adding it) does that.
export default function RecycleBin() {
  const [bin, setBin] = useState<TrashList | null>(null)
  const [busy, setBusy] = useState('')
  const confirm = useConfirm()
  const toast = useToast()

  const load = useCallback(() => {
    api
      .listTrash()
      .then(setBin)
      .catch((e) => toast.error(e instanceof Error ? e.message : String(e)))
  }, [toast])
  useEffect(load, [load])

  async function run(key: string, fn: () => Promise<unknown>, done: string) {
    setBusy(key)
    try {
      await fn()
      toast.success(done)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  if (!bin) return null
  const days = bin.days
  return (
    <>
      <p style={{ color: 'var(--text-dim)' }}>
        {days > 0
          ? `When you remove a title with its files, the files wait here for ${days} ${days === 1 ? 'day' : 'days'} before they are deleted for good. Put them back if it was a mistake.`
          : 'The recycle bin is off: removed files are deleted straight away. Switch it on under Settings > System > Clean up.'}
      </p>
      {bin.items.length === 0 ? (
        <div className="empty-state">
          <Icon name="trash" size={44} />
          <p>The recycle bin is empty.</p>
        </div>
      ) : (
        <>
          <div style={{ marginBottom: 12 }}>
            <button
              className="btn-sm btn-danger"
              disabled={busy !== ''}
              onClick={async () => {
                if (!(await confirm({ title: 'Empty the recycle bin?', body: <p>Every file in it is deleted for good. This cannot be undone.</p>, confirmLabel: 'Empty it', danger: true }))) return
                await run('all', () => api.emptyTrash(), 'The recycle bin is empty.')
              }}
            >
              Empty the recycle bin
            </button>
          </div>
          <table>
            <thead>
              <tr>
                <th>Removed</th>
                <th>Size</th>
                <th>When</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {bin.items.map((it) => {
                const key = `${it.kind}/${it.id}`
                return (
                  <tr key={key}>
                    <td>
                      <strong>{it.label}</strong>
                      <div style={{ color: 'var(--text-dim)', fontSize: '0.82rem', wordBreak: 'break-all' }}>{it.paths[0]}</div>
                    </td>
                    <td>
                      {formatBytes(it.size)}
                      <div style={{ color: 'var(--text-dim)', fontSize: '0.82rem' }}>{it.files === 1 ? '1 file' : `${it.files} files`}</div>
                    </td>
                    <td>
                      {timeAgo(it.deletedAt)}
                      {it.expiresAt && <div style={{ color: 'var(--text-dim)', fontSize: '0.82rem' }}>deleted for good {inDays(it.expiresAt)}</div>}
                    </td>
                    <td className="row-actions">
                      <button
                        className="btn-sm btn-with-icon"
                        disabled={busy !== ''}
                        onClick={() => void run(key, () => api.restoreTrash(it.kind, it.id), `${it.label} is back in its folder. Use Import in the Library to add it to your library again.`)}
                      >
                        <Icon name="refresh" size={14} /> Put back
                      </button>
                      <button
                        className="btn-sm"
                        disabled={busy !== ''}
                        onClick={async () => {
                          if (!(await confirm({ title: `Delete ${it.label} for good?`, body: <p>The files are deleted now. This cannot be undone.</p>, confirmLabel: 'Delete', danger: true }))) return
                          await run(key, () => api.deleteTrash(it.kind, it.id), 'Deleted for good.')
                        }}
                      >
                        Delete now
                      </button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </>
      )}
    </>
  )
}

function inDays(iso: string): string {
  const days = Math.ceil((new Date(iso).getTime() - Date.now()) / 86400000)
  if (days <= 0) return 'today'
  if (days === 1) return 'tomorrow'
  return `in ${days} days`
}
