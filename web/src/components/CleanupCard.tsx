import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type CleanupReport } from '../api'
import { formatBytes, timeAgo } from '../format'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'

const REASON: Record<string, string> = {
  orphaned: 'Not part of any download',
  'imported-leftover': 'Left over after importing',
  'seeding-finished': 'Torrent finished seeding',
  'empty-folder': 'Empty folder',
}

// System → Clean up: what can be removed from the downloads area (never the
// library), with a button to do it now and a switch to do it daily.
export default function CleanupCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [report, setReport] = useState<CleanupReport | null>(null)
  const [missing, setMissing] = useState(false)
  const [running, setRunning] = useState(false)

  const load = useCallback(() => {
    api
      .cleanupReport()
      .then(setReport)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) setMissing(true)
      })
  }, [])
  useEffect(load, [load])

  if (missing) return null

  async function runNow() {
    if (!report) return
    if (!(await confirm({ title: `Free ${formatBytes(report.reclaimableBytes)}?`, body: <p>Removes the {report.items.length} item{report.items.length === 1 ? '' : 's'} listed from the downloads folder. Nothing in your movie or TV library is touched.</p>, confirmLabel: 'Clean up now', danger: true }))) return
    setRunning(true)
    try {
      const r = await api.runCleanup()
      toast.success(`Cleaned up: ${formatBytes(r.removedBytes ?? 0)} freed.`)
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setRunning(false)
    }
  }

  async function setAuto(auto: boolean) {
    try {
      await api.putSettings({ cleanupAuto: auto })
      toast.success(auto ? 'Mediarium now cleans up once a day.' : 'Automatic clean-up is off.')
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <fieldset className="group usenet span-all">
      <legend>
        <Icon name="trash" size={14} /> Clean up
      </legend>
      <p style={{ marginTop: 0 }}>
        Leftovers in the downloads folder: files from failed or finished downloads, torrents that are done seeding, and empty folders.
        Your movie and TV library is never touched.
      </p>
      {report === null ? (
        <div className="skeleton" style={{ height: 80 }} />
      ) : (
        <>
          <div className="cleanup-head">
            <div>
              <strong className="cleanup-size">{formatBytes(report.reclaimableBytes)}</strong> can be freed
              {report.lastRunAt && <small> · last cleaned {timeAgo(report.lastRunAt)}</small>}
            </div>
            <button className="primary btn-with-icon" onClick={() => void runNow()} disabled={running || report.items.length === 0}>
              <Icon name="trash" size={15} /> {running ? 'Cleaning…' : 'Clean up now'}
            </button>
          </div>
          {report.items.length > 0 && (
            <ul className="file-list cleanup-list">
              {report.items.slice(0, 50).map((it) => (
                <li key={it.path}>
                  <span className="file-ico">
                    <Icon name="folder" size={16} />
                  </span>
                  <div className="file-name">
                    <span title={it.path}>{it.path}</span>
                    <small>
                      {REASON[it.reason] ?? it.reason} · {formatBytes(it.sizeBytes)}
                    </small>
                  </div>
                </li>
              ))}
            </ul>
          )}
          <div style={{ marginTop: 12 }}>
            <Switch checked={report.auto} onChange={(v) => void setAuto(v)} label="Clean up automatically once a day" description="Only leftovers like the ones above; downloads still in progress or seeding are left alone." />
          </div>
        </>
      )}
    </fieldset>
  )
}
