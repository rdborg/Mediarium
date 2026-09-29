import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Settings } from '../../api'
import CleanupCard from '../../components/CleanupCard'
import Icon from '../../components/Icon'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/ConfirmProvider'

// Backup and restore, plus the legal notice you accepted. Your name, email and
// password live on the Profile page.
export default function SystemSettings() {
  const confirm = useConfirm()
  const toast = useToast()
  const [s, setS] = useState<Settings | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [restoring, setRestoring] = useState(false)
  const [waiting, setWaiting] = useState(false)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    api.getSettings().then(setS).catch(() => undefined)
  }, [])

  async function restore() {
    if (!file) return
    if (!(await confirm({ title: 'Restore this backup?', body: <p>It replaces your library, accounts and settings with the ones in the file. Your current data is kept in a &quot;before-restore&quot; folder inside your config folder, and Mediarium restarts.</p>, confirmLabel: 'Restore and restart', danger: true }))) return
    setRestoring(true)
    try {
      const body = new FormData()
      body.append('file', file)
      const res = await fetch('/api/system/restore', { method: 'POST', body, credentials: 'include' })
      const text = await res.text()
      const data = text ? JSON.parse(text) : null
      if (!res.ok) throw new Error(data?.error ?? `Restore failed (${res.status}).`)
      toast.success('Backup accepted. Restarting…')
      setWaiting(true)
      // Wait for the app to come back, then reload onto the restored data.
      const started = Date.now()
      const poll = setInterval(async () => {
        try {
          const r = await fetch('/api/version', { cache: 'no-store' })
          if (r.ok && Date.now() - started > 3000) {
            clearInterval(poll)
            window.location.href = '/'
          }
        } catch {
          // still restarting
        }
        if (Date.now() - started > 90000) {
          clearInterval(poll)
          setWaiting(false)
          toast.info('Mediarium did not come back on its own. If it is not running, start it again and reload this page.')
        }
      }, 2000)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setRestoring(false)
    }
  }

  return (
    <div className="settings-stack">
      <CleanupCard />
      <fieldset className="group folders">
        <legend>
          <Icon name="hard" size={14} /> Backup
        </legend>
        <p style={{ marginTop: 0 }}>
          A backup is one file with your library, accounts, indexers, provider logins and settings. Your movie and TV files are not in it. It also contains the key that unlocks your saved passwords, so keep it somewhere private.
        </p>
        <a className="btn-link primary-look" href="/api/system/backup" download>
          <Icon name="download" size={16} /> Download a backup
        </a>
      </fieldset>

      <fieldset className="group alerts">
        <legend>
          <Icon name="refresh" size={14} /> Restore
        </legend>
        <p style={{ marginTop: 0 }}>
          Choose a backup file to go back to it. Mediarium restarts to load it (Docker starts it again by itself). Your current data is not deleted: it is moved into a <code>before-restore</code> folder in your config folder.
        </p>
        <div className="restore-row">
          <input ref={input} type="file" accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          <button className="danger-ghost btn-with-icon" onClick={() => void restore()} disabled={!file || restoring}>
            <Icon name="refresh" size={15} /> {waiting ? 'Restarting…' : restoring ? 'Uploading…' : 'Restore this backup'}
          </button>
        </div>
      </fieldset>

      <fieldset className="group torrent">
        <legend>
          <Icon name="shield" size={14} /> Legal notice
        </legend>
        <p style={{ marginTop: 0 }}>
          Mediarium is an automation and organising tool for content you have the legal right to obtain. It does not host, index or supply any content. You alone are responsible for what you download and for following the laws of your country and the terms of your providers.
        </p>
        <p style={{ margin: 0, color: 'var(--text-dim)' }}>
          {s?.legalAcknowledgedAt ? `You accepted this on ${new Date(s.legalAcknowledgedAt).toLocaleDateString()}.` : 'Not yet accepted.'} The full wording is on the <Link to="/settings/about">About &amp; Credits</Link> page.
        </p>
      </fieldset>
    </div>
  )
}
