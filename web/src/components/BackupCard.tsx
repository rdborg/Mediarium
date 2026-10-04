import { useCallback, useEffect, useState } from 'react'
import { api, type SavedBackups } from '../api'
import { formatBytes, timeAgo } from '../format'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'

// Settings > System > Backup: download one now, and the backups Mediarium
// saves by itself every night and before each update.
export default function BackupCard() {
  const toast = useToast()
  const [data, setData] = useState<SavedBackups | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .savedBackups()
      .then(setData)
      .catch(() => undefined)
  }, [])
  useEffect(load, [load])

  async function backUpNow() {
    setBusy(true)
    try {
      await api.saveBackupNow()
      toast.success('Backup saved.')
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function save(change: { backupAuto?: boolean; backupKeep?: number }, done: string) {
    try {
      await api.putSettings(change)
      toast.success(done)
      load()
    } catch (e) {
      toast.error(`Not saved: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  const latest = data?.items[0]
  return (
    <fieldset className="group folders">
      <legend>
        <Icon name="hard" size={14} /> Backup
      </legend>
      <p style={{ marginTop: 0 }}>
        One file with your library, accounts, indexers, provider logins and settings, but not your media files. It also holds the key to your saved passwords, so keep it private.
      </p>
      <div className="backup-actions">
        <a className="btn-link primary-look" href="/api/system/backup" download>
          <Icon name="download" size={16} /> Download a backup
        </a>
        <button className="btn-with-icon" onClick={() => void backUpNow()} disabled={busy}>
          <Icon name="hard" size={15} /> {busy ? 'Saving…' : 'Back up now'}
        </button>
      </div>
      {data && (
        <>
          <div style={{ marginTop: 14 }}>
            <Switch
              checked={data.auto}
              onChange={(v) => void save({ backupAuto: v }, v ? 'Nightly backups are on.' : 'Nightly backups are off.')}
              label="Save a backup every night"
              description={`Kept in ${data.folder}, with one more before every update. The newest ${data.keep} are kept.`}
              showState
            />
          </div>
          <label className="inline-field" style={{ marginTop: 10 }}>
            Keep the newest
            <select value={data.keep} onChange={(e) => void save({ backupKeep: Number(e.target.value) }, `Saved: the newest ${e.target.value} backups are kept.`)}>
              {[3, 5, 7, 14, 30].map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            backups
          </label>
          <p className="field-hint" style={{ marginTop: 10 }}>
            {latest ? `Last backup ${timeAgo(latest.createdAt)}.` : 'No backup has been saved yet.'} To restore one, download it here and choose it under Restore.
          </p>
          {data.items.length > 0 && (
            <ul className="file-list">
              {data.items.map((b) => (
                <li key={b.name}>
                  <span className="file-ico">
                    <Icon name="hard" size={16} />
                  </span>
                  <div className="file-name">
                    <span>{new Date(b.createdAt).toLocaleString()}</span>
                    <small>{formatBytes(b.size)}</small>
                  </div>
                  <a className="btn-sm" href={`/api/system/backups/${encodeURIComponent(b.name)}`} download>
                    Download
                  </a>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </fieldset>
  )
}
