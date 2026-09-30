import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Settings } from '../../api'
import CleanupCard from '../../components/CleanupCard'
import ServerStatsCard from '../../components/ServerStats'
import SupportCard from '../../components/SupportCard'
import { PushCard, RestartCard, UpdatesCard } from '../../components/UpdateCards'
import Icon from '../../components/Icon'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/ConfirmProvider'
import { FieldError, useValidation } from '../../useValidation'

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
  const errors = {
    file: file
      ? /\.zip$/i.test(file.name)
        ? null
        : "That isn't a Mediarium backup. Choose the .zip you got from Download a backup."
      : null,
  }
  const v = useValidation(errors)

  useEffect(() => {
    api.getSettings().then(setS).catch(() => undefined)
  }, [])

  async function restore() {
    if (!file || !v.attempt()) return
    if (!(await confirm({ title: 'Restore this backup?', body: <p>It replaces your library, accounts and settings with the ones in the file. Your current data goes into a &quot;before-restore&quot; folder in your config folder, and Mediarium restarts.</p>, confirmLabel: 'Restore and restart', danger: true }))) return
    setRestoring(true)
    try {
      const body = new FormData()
      body.append('file', file)
      const res = await fetch('/api/system/restore', { method: 'POST', body, credentials: 'include' })
      const text = await res.text()
      let data: { error?: string } | null = null
      try {
        data = text ? JSON.parse(text) : null
      } catch {
        // A proxy in front can answer with a web page instead of JSON.
      }
      if (!res.ok) throw new Error(data?.error ?? `The restore failed (error ${res.status}). If the backup is big, make sure any proxy in front of Mediarium allows large uploads.`)
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
          setRestoring(false)
          toast.info("Mediarium didn't come back. Start it again and reload this page.")
        }
      }, 2000)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setRestoring(false)
    }
  }

  return (
    <div className="settings-stack">
      <ServerStatsCard />
      <UpdatesCard />
      <RestartCard />
      <PushCard />
      <CleanupCard />
      <SupportCard />
      <fieldset className="group folders">
        <legend>
          <Icon name="hard" size={14} /> Backup
        </legend>
        <p style={{ marginTop: 0 }}>
          One file with your library, accounts, indexers, provider logins and settings, but not your media files. It also holds the key to your saved passwords, so keep it private.
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
          Choose a backup file. Mediarium restarts to load it, and your current data moves to a <code>before-restore</code> folder in your config folder.
        </p>
        <div className="restore-row">
          <input ref={input} type="file" accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] ?? null)} {...v.bind('file')} />
          <button className="danger-ghost btn-with-icon" onClick={() => void restore()} disabled={!file || restoring}>
            <Icon name="refresh" size={15} /> {waiting ? 'Restarting…' : restoring ? 'Uploading…' : 'Restore this backup'}
          </button>
        </div>
        <FieldError v={v} name="file" />
      </fieldset>

      <fieldset className="group torrent">
        <legend>
          <Icon name="shield" size={14} /> Legal notice
        </legend>
        <p style={{ marginTop: 0 }}>
          Mediarium is an automation and organising tool for content you have the legal right to obtain. It doesn't host, index or supply any content. You alone are responsible for what you download and for following the laws of your country and the terms of your providers.
        </p>
        <p style={{ margin: 0, color: 'var(--text-dim)' }}>
          {s?.legalAcknowledgedAt ? `You accepted this on ${new Date(s.legalAcknowledgedAt).toLocaleDateString()}.` : 'Not yet accepted.'} The full wording is on the <Link to="/settings/about">About and credits</Link> page.
        </p>
      </fieldset>
    </div>
  )
}
