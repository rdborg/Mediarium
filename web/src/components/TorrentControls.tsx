import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type QueueItem, type TorrentDetail, type TorrentFile } from '../api'
import { formatBytes } from '../format'
import { plural, t } from '../i18n'
import { useToast } from './Toast'
import '../torrentcontrols.css'

// The folder every file of a torrent sits in is the same for all of them, so it
// only adds length to the list.
function shortPaths(files: TorrentFile[]): string[] {
  if (files.length < 2) return files.map((f) => f.path.split('/').pop() ?? f.path)
  const first = files[0].path.split('/')[0]
  const shared = files.every((f) => f.path.includes('/') && f.path.split('/')[0] === first)
  return files.map((f) => (shared ? f.path.slice(first.length + 1) : f.path))
}

const kbText = (n: number) => (n > 0 ? String(n) : '')
const kbValue = (s: string) => {
  const n = Number(s.trim().replace(',', '.'))
  return Number.isFinite(n) && n > 0 ? Math.round(n) : 0
}

// What a person can change for one torrent of the built-in client, under
// Details in the download list: whether it downloads in order, its own speed
// limits, and which of its files are downloaded. The server keeps the choices,
// so they are still there after a restart.
export default function TorrentControls({ q, open }: { q: QueueItem; open: boolean }) {
  const toast = useToast()
  const [d, setD] = useState<TorrentDetail | null>(null)
  const [down, setDown] = useState('')
  const [up, setUp] = useState('')
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  // Read inside the refresh below: a limit that is being typed must survive the
  // download changing state.
  const dirtyRef = useRef(false)
  dirtyRef.current = dirty

  const take = useCallback((next: TorrentDetail, resetLimits: boolean) => {
    setD(next)
    if (resetLimits) {
      setDown(kbText(next.downLimitKB))
      setUp(kbText(next.upLimitKB))
      setDirty(false)
    }
  }, [])

  // Read when Details is opened, then keep the progress of the files current.
  useEffect(() => {
    if (!open) return
    let gone = false
    const load = (first: boolean) =>
      api
        .torrentDetail(q.id)
        .then((x) => {
          if (!gone) take(x, first && !dirtyRef.current)
        })
        .catch(() => undefined)
    void load(true)
    const timer = q.status === 'downloading' ? setInterval(() => void load(false), 3000) : undefined
    return () => {
      gone = true
      if (timer) clearInterval(timer)
    }
  }, [open, q.id, q.status, take])

  if (!d) return null
  // A finished torrent that is no longer seeding has nothing to steer.
  if (!d.live && q.status === 'completed') return null

  async function change(patch: { sequential?: TorrentDetail['sequential']; downLimitKB?: number; upLimitKB?: number }, done: string) {
    setBusy(true)
    try {
      take(await api.setTorrentDetail(q.id, patch), patch.downLimitKB !== undefined || patch.upLimitKB !== undefined)
      toast.success(done)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function tick(file: TorrentFile, wanted: boolean) {
    if (!d) return
    const skip = d.files.filter((f) => (f.path === file.path ? !wanted : !f.wanted)).map((f) => f.path)
    const before = d
    // Show the tick at once; the server's answer replaces it, or it goes back.
    setD({ ...d, files: d.files.map((f) => (f.path === file.path ? { ...f, wanted } : f)) })
    setBusy(true)
    try {
      take(await api.setTorrentFiles(q.id, skip), false)
    } catch (e) {
      setD(before)
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const labels = d.files.length > 1 ? shortPaths(d.files) : []
  const left = d.files.filter((f) => f.wanted).length
  const sequentialName = d.sequentialDefault ? t('on') : t('off')
  const editable = d.live && d.filesOpen

  return (
    <div className="torrent-controls">
      <h4>{t('Torrent controls')}</h4>
      {!d.live && <p className="hint">{t('The file list and the live settings are here while the built-in client downloads or seeds this torrent. What you set now is used when it starts.')}</p>}
      <div className="tc-row">
        <label className="tc-field">
          <span>{t('Download in order')}</span>
          <select value={d.sequential} disabled={busy} onChange={(e) => void change({ sequential: e.target.value as TorrentDetail['sequential'] }, t('Saved.'))}>
            <option value="default">{t('Use the default ({state})', { state: sequentialName })}</option>
            <option value="on">{t('On')}</option>
            <option value="off">{t('Off')}</option>
          </select>
          <small>{t('First piece to last, so a video can be played while it downloads.')}</small>
        </label>
        <div className="tc-field">
          <span>{t('Speed limits for this torrent')}</span>
          <div className="tc-limits">
            <label>
              <small>{t('Download KB/s')}</small>
              <input
                inputMode="numeric"
                value={down}
                placeholder={t('No limit')}
                onChange={(e) => {
                  setDown(e.target.value)
                  setDirty(true)
                }}
              />
            </label>
            <label>
              <small>{t('Upload KB/s')}</small>
              <input
                inputMode="numeric"
                value={up}
                placeholder={t('No limit')}
                onChange={(e) => {
                  setUp(e.target.value)
                  setDirty(true)
                }}
              />
            </label>
            <button className="btn-sm" disabled={busy || !dirty} onClick={() => void change({ downLimitKB: kbValue(down), upLimitKB: kbValue(up) }, t('Speed limits saved.'))}>
              {t('Save')}
            </button>
          </div>
          <small>{t('The limits in Settings still apply on top of these.')}</small>
        </div>
      </div>
      {d.live && d.files.length > 1 && (
        <fieldset className="tc-files">
          <legend>{plural(left, '{n} file will be downloaded', '{n} files will be downloaded')}</legend>
          {!editable && <p className="hint">{t('The files are chosen while the torrent downloads.')}</p>}
          <ul>
            {d.files.map((f, i) => (
              <li key={f.path} className={f.wanted ? '' : 'skipped'}>
                <label>
                  <input type="checkbox" checked={f.wanted} disabled={busy || !editable} onChange={(e) => void tick(f, e.target.checked)} />
                  <span className="tc-name" title={f.path}>
                    {labels[i]}
                  </span>
                </label>
                <span className="tc-size">{formatBytes(f.size)}</span>
                <span className="tc-done">{f.wanted ? `${f.size > 0 ? Math.floor((f.done / f.size) * 100) : 100}%` : t('left out')}</span>
              </li>
            ))}
          </ul>
        </fieldset>
      )}
    </div>
  )
}
