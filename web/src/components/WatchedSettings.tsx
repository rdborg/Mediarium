import { useCallback, useEffect, useState } from 'react'
import { api, type LibraryCleanupItem, type CleanupRules, type WatchedSettings as Settings } from '../api'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'
import { useConfirm } from './ConfirmProvider'

// Settings > Connections > Media servers: reading what's been watched from
// Plex, Jellyfin and Emby, and the cleanup rules built on it. Both are off
// until switched on; cleanup always shows what it would remove first.

function when(iso?: string): string {
  if (!iso) return 'never'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? 'never' : d.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

function DaysRule({ label, hint, value, onChange, disabled }: { label: string; hint: string; value: number; onChange: (v: number) => void; disabled?: boolean }) {
  const on = value > 0
  return (
    <div className="cleanup-rule">
      <Switch checked={on} onChange={(v) => onChange(v ? 30 : 0)} disabled={disabled} label={label} description={hint} />
      {on && (
        <label className="cleanup-days">
          after
          <input type="number" min={1} max={3650} value={value} disabled={disabled} onChange={(e) => onChange(Math.max(1, Math.min(3650, Number(e.target.value) || 1)))} />
          days
        </label>
      )}
    </div>
  )
}

export default function WatchedSettings() {
  const toast = useToast()
  const confirm = useConfirm()
  const [st, setSt] = useState<Settings | null>(null)
  const [rules, setRules] = useState<CleanupRules | null>(null)
  const [tags, setTags] = useState('')
  const [busy, setBusy] = useState('')
  const [preview, setPreview] = useState<LibraryCleanupItem[] | null>(null)

  const load = useCallback(() => {
    api
      .watchedSettings()
      .then((s) => {
        setSt(s)
        setRules(s.cleanup)
        setTags(s.cleanup.keepTags.join(', '))
      })
      .catch(() => setSt(null))
  }, [])
  useEffect(load, [load])

  async function run(label: string, fn: () => Promise<void>) {
    setBusy(label)
    try {
      await fn()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy('')
    }
  }

  const draft = (): CleanupRules => ({ ...(rules as CleanupRules), keepTags: tags.split(',').map((t) => t.trim()).filter(Boolean) })

  if (!st || !rules) return null
  const status = st.status
  const anyRule = rules.moviesWatchedDays > 0 || rules.moviesUnwatchedDays > 0 || rules.episodesWatchedDays > 0
  return (
    <>
      <fieldset className="group span-all">
        <legend>
          <Icon name="eye" size={14} /> What&apos;s been watched
        </legend>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          Mediarium can ask Plex, Jellyfin and Emby what has been played, every six hours, and show it on title pages. Plex tells it what the account
          connected above has watched; Jellyfin and Emby tell it about all their users. Nothing is changed on your media servers.
        </p>
        <Switch
          checked={st.sync}
          disabled={!!busy}
          onChange={(v) =>
            void run('sync-switch', async () => {
              const s = await api.putWatchedSettings({ sync: v })
              setSt(s)
              setRules(s.cleanup)
              toast.success(v ? 'Mediarium reads what has been watched now and every six hours.' : 'No longer reading what has been watched. Cleanup is off too.')
              if (v) {
                await api.syncWatched()
                load()
              }
            })
          }
          label="Read what's been watched"
          description="Off unless you switch it on."
        />
        {st.sync && (
          <div className="row-actions" style={{ marginTop: 10, alignItems: 'center' }}>
            <span className="hint">
              Last read {when(status.lastSync)}
              {status.lastSync ? ` · ${status.movies} movies and ${status.episodes} episodes watched` : ''}
            </span>
            <button
              className="btn-sm btn-with-icon"
              disabled={!!busy}
              onClick={() =>
                void run('sync', async () => {
                  const s = await api.syncWatched()
                  toast.success(`Read from ${s.servers} media ${s.servers === 1 ? 'server' : 'servers'}: ${s.movies} movies and ${s.episodes} episodes watched.`)
                  load()
                })
              }
            >
              <Icon name="refresh" size={14} /> {busy === 'sync' ? 'Reading…' : 'Read now'}
            </button>
          </div>
        )}
        {status.lastError && <p className="error-text">{status.lastError}</p>}
      </fieldset>

      {st.sync && (
        <fieldset className="group span-all">
          <legend>
            <Icon name="trash" size={14} /> Cleanup rules
          </legend>
          <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
            Free up space by removing what has been watched. Once a day the files of titles matching a rule are deleted (into the recycle bin when it is
            on), the title stays in your library, and Mediarium stops looking for it. Every removal is listed under Activity. Check the preview before
            you switch it on.
          </p>
          <DaysRule
            label="Movies watched a while ago"
            hint="Remove a movie once it was last watched this many days ago."
            value={rules.moviesWatchedDays}
            onChange={(v) => setRules({ ...rules, moviesWatchedDays: v })}
            disabled={!!busy}
          />
          <DaysRule
            label="Movies nobody watched"
            hint="Remove a movie that was never watched, this many days after it was added."
            value={rules.moviesUnwatchedDays}
            onChange={(v) => setRules({ ...rules, moviesUnwatchedDays: v })}
            disabled={!!busy}
          />
          <DaysRule
            label="Episodes watched a while ago"
            hint="Remove an episode once it was last watched this many days ago."
            value={rules.episodesWatchedDays}
            onChange={(v) => setRules({ ...rules, episodesWatchedDays: v })}
            disabled={!!busy}
          />
          <label className="cleanup-keep">
            <span>Always keep titles tagged</span>
            <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="Keep, Kids" disabled={!!busy} />
            <small className="hint">Separate tags with commas. A tagged movie or show is never removed.</small>
          </label>
          <div className="row-actions" style={{ marginTop: 12 }}>
            <button
              className="btn-with-icon"
              disabled={!!busy || !anyRule}
              onClick={() =>
                void run('preview', async () => {
                  const p = await api.cleanupPreview(draft())
                  setPreview(p.items)
                })
              }
            >
              <Icon name="eye" size={15} /> {busy === 'preview' ? 'Checking…' : 'Preview'}
            </button>
            <button
              className="primary btn-with-icon"
              disabled={!!busy}
              onClick={() =>
                void run('save', async () => {
                  const s = await api.putWatchedSettings({ cleanup: draft() })
                  setSt(s)
                  setRules(s.cleanup)
                  setTags(s.cleanup.keepTags.join(', '))
                  toast.success('Cleanup rules saved.')
                })
              }
            >
              Save rules
            </button>
            <Switch
              checked={rules.enabled}
              disabled={!!busy || !anyRule}
              onChange={(v) =>
                void run('enable', async () => {
                  if (v && !(await confirm({ title: 'Switch cleanup on?', body: 'Once a day Mediarium deletes the files of titles that match these rules. Check the preview first.', confirmLabel: 'Switch on', danger: true })))
                    return
                  const s = await api.putWatchedSettings({ cleanup: { ...draft(), enabled: v } })
                  setSt(s)
                  setRules(s.cleanup)
                  toast.success(v ? 'Cleanup is on. It runs once a day.' : 'Cleanup is off.')
                })
              }
              label="Run once a day"
            />
          </div>
          {preview && (
            <div className="cleanup-preview">
              {preview.length === 0 ? (
                <p className="hint">Nothing matches these rules right now.</p>
              ) : (
                <>
                  <p className="hint">
                    {preview.length === 1 ? 'One title matches' : `${preview.length} titles match`}. Nothing has been removed.
                    {preview.length > 50 ? ' A run removes up to 50 at a time.' : ''}
                  </p>
                  <ul>
                    {preview.slice(0, 100).map((it) => (
                      <li key={`${it.kind}-${it.id}`}>
                        <Icon name={it.kind === 'movie' ? 'film' : 'tv'} size={13} /> <strong>{it.title}</strong> <span className="hint">· {it.reason}</span>
                      </li>
                    ))}
                  </ul>
                  <button
                    className="btn-sm danger-ghost btn-with-icon"
                    disabled={!!busy}
                    onClick={() =>
                      void run('run', async () => {
                        if (!(await confirm({ title: 'Remove these now?', body: 'The files of the titles listed are deleted now (into the recycle bin when it is on).', confirmLabel: 'Remove now', danger: true })))
                          return
                        await api.putWatchedSettings({ cleanup: draft() })
                        const r = await api.runLibraryCleanup()
                        toast.success(`Removed ${r.removed.length} ${r.removed.length === 1 ? 'title' : 'titles'}${r.failed.length ? `, ${r.failed.length} couldn't be removed` : ''}.`)
                        setPreview(null)
                        load()
                      })
                    }
                  >
                    <Icon name="trash" size={14} /> Remove these now
                  </button>
                </>
              )}
            </div>
          )}
          <p className="hint" style={{ marginBottom: 0 }}>
            Last cleanup {when(status.lastCleanup)}.
          </p>
        </fieldset>
      )}
    </>
  )
}
