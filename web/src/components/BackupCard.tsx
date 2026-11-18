import { useCallback, useEffect, useState } from 'react'
import { api, type SavedBackups } from '../api'
import { formatBytes, timeAgo } from '../format'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'
import { locale, t, tx } from '../i18n'
import { withBase } from '../base'
import TipLabel from './TipLabel'

// Settings > System > Backup and restore: download one now, and the backups Mediarium
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
      toast.success(t('Backup saved.'))
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
      toast.error(t('Not saved: {error}', { error: e instanceof Error ? e.message : String(e) }))
    }
  }

  const latest = data?.items[0]
  return (
    <fieldset className="group folders">
      <legend>
        <Icon name="hard" size={14} /> <TipLabel help="backups-updates#backup-and-restore">{t('Backup')}</TipLabel>
      </legend>
      <p style={{ marginTop: 0 }}>
        {t('One file with your library, accounts, indexers, provider logins and settings, but not your media files. It also holds the key to your saved passwords, so keep it private.')}
      </p>
      <div className="backup-actions">
        <a className="btn-link primary-look" href={withBase('/api/system/backup')} download>
          <Icon name="download" size={16} /> {t('Download a backup')}
        </a>
        <button className="btn-with-icon" onClick={() => void backUpNow()} disabled={busy}>
          <Icon name="hard" size={15} /> {busy ? t('Saving…') : t('Back up now')}
        </button>
      </div>
      {data && (
        <>
          <div style={{ marginTop: 14 }}>
            <Switch
              checked={data.auto}
              onChange={(v) => void save({ backupAuto: v }, v ? t('Nightly backups are on.') : t('Nightly backups are off.'))}
              label={t('Save a backup every night')}
              description={t('Kept in {folder}, with one more before every update. The newest {n} are kept.', { folder: data.folder, n: data.keep })}
              showState
            />
          </div>
          <label className="inline-field" style={{ marginTop: 10 }}>
            {tx('Keep the newest {count} backups', {
              count: (
                <select value={data.keep} onChange={(e) => void save({ backupKeep: Number(e.target.value) }, t('Saved: the newest {n} backups are kept.', { n: e.target.value }))}>
                  {[3, 5, 7, 14, 30].map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </select>
              ),
            })}
          </label>
          <p className="field-hint" style={{ marginTop: 10 }}>
            {latest ? t('Last backup {when}.', { when: timeAgo(latest.createdAt) }) : t('No backup has been saved yet.')} {t('To restore one, download it here and choose it under Restore.')}
          </p>
          {data.items.length > 0 && (
            <ul className="file-list">
              {data.items.map((b) => (
                <li key={b.name}>
                  <span className="file-ico">
                    <Icon name="hard" size={16} />
                  </span>
                  <div className="file-name">
                    <span className="file-when">{new Date(b.createdAt).toLocaleString(locale())}</span>
                    <small>{formatBytes(b.size)}</small>
                  </div>
                  <a className="btn-sm" href={withBase(`/api/system/backups/${encodeURIComponent(b.name)}`)} download>
                    {t('Download')}
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
