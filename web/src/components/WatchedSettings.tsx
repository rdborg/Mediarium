import { useCallback, useEffect, useState } from 'react'
import { api, type LibraryCleanupItem, type CleanupRules, type WatchedSettings as Settings } from '../api'
import { locale, plural, t, st as serverText } from '../i18n'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'
import { useConfirm } from './ConfirmProvider'
import TipLabel from './TipLabel'

// Settings > Connections > Media servers: reading what's been watched from
// Plex, Jellyfin and Emby, and the cleanup rules built on it. Both are off
// until switched on; cleanup always shows what it would remove first.

export function when(iso?: string): string {
  if (!iso) return t('never')
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? t('never') : d.toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

export function DaysRule({ label, hint, value, onChange, disabled }: { label: string; hint: string; value: number; onChange: (v: number) => void; disabled?: boolean }) {
  const on = value > 0
  // The number box sits inside the phrase, so split it around the box.
  const [before, after] = t('after {days} days').split('{days}').map((s) => s.trim())
  return (
    <div className="cleanup-rule">
      <Switch checked={on} onChange={(v) => onChange(v ? 30 : 0)} disabled={disabled} label={label} description={hint} />
      {on && (
        <label className="cleanup-days">
          {before}
          <input type="number" min={1} max={3650} value={value} disabled={disabled} onChange={(e) => onChange(Math.max(1, Math.min(3650, Number(e.target.value) || 1)))} />
          {after}
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
  // The rules the preview was made with: "Remove these now" only runs them.
  const [previewRules, setPreviewRules] = useState('')

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
          <Icon name="eye" size={14} /> <TipLabel help="media-servers#whats-been-watched">{t("What's been watched")}</TipLabel>
        </legend>
        <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
          {t('Mediarium can ask Plex, Jellyfin and Emby what has been played, every six hours, and show it on title pages. Plex tells it what the account connected above has watched; Jellyfin and Emby tell it about all their users. Nothing is changed on your media servers.')}
        </p>
        <Switch
          checked={st.sync}
          disabled={!!busy}
          onChange={(v) =>
            void run('sync-switch', async () => {
              const s = await api.putWatchedSettings({ sync: v })
              setSt(s)
              setRules(s.cleanup)
              toast.success(v ? t('Mediarium reads what has been watched now and every six hours.') : t('No longer reading what has been watched. Cleanup is off too.'))
              if (v) {
                await api.syncWatched()
                load()
              }
            })
          }
          label={t("Read what's been watched")}
          description={t('Off unless you switch it on.')}
        />
        {st.sync && (
          <div className="row-actions" style={{ marginTop: 10, alignItems: 'center' }}>
            <span className="hint">
              {t('Last read {when}', { when: when(status.lastSync) })}
              {status.lastSync
                ? ` · ${t('{movies} and {episodes} watched', { movies: plural(status.movies, '{n} movie', '{n} movies'), episodes: plural(status.episodes, '{n} episode', '{n} episodes') })}`
                : ''}
            </span>
            <button
              className="btn-sm btn-with-icon"
              disabled={!!busy}
              onClick={() =>
                void run('sync', async () => {
                  const s = await api.syncWatched()
                  toast.success(
                    plural(s.servers, 'Read from {n} media server: {movies} and {episodes} watched.', 'Read from {n} media servers: {movies} and {episodes} watched.', {
                      movies: plural(s.movies, '{n} movie', '{n} movies'),
                      episodes: plural(s.episodes, '{n} episode', '{n} episodes'),
                    }),
                  )
                  load()
                })
              }
            >
              <Icon name="refresh" size={14} /> {busy === 'sync' ? t('Reading…') : t('Read now')}
            </button>
          </div>
        )}
        {status.lastError && <p className="error-text">{status.lastError}</p>}
      </fieldset>

      {st.sync && (
        <fieldset className="group span-all">
          <legend>
            <Icon name="trash" size={14} /> <TipLabel help="media-servers#cleanup-rules">{t('Cleanup rules')}</TipLabel>
          </legend>
          <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
            {t('Free up space by removing what has been watched. Once a day the files of titles matching a rule are deleted (into the recycle bin when it is on), the title stays in your library, and Mediarium stops looking for it. Every removal is listed under Activity. Check the preview before you switch it on.')}
          </p>
          <DaysRule
            label={t('Movies watched a while ago')}
            hint={t('Remove a movie once it was last watched this many days ago.')}
            value={rules.moviesWatchedDays}
            onChange={(v) => setRules({ ...rules, moviesWatchedDays: v })}
            disabled={!!busy}
          />
          <DaysRule
            label={t('Movies nobody watched')}
            hint={t('Remove a movie that was never watched, this many days after it was added.')}
            value={rules.moviesUnwatchedDays}
            onChange={(v) => setRules({ ...rules, moviesUnwatchedDays: v })}
            disabled={!!busy}
          />
          <DaysRule
            label={t('Episodes watched a while ago')}
            hint={t('Remove an episode once it was last watched this many days ago.')}
            value={rules.episodesWatchedDays}
            onChange={(v) => setRules({ ...rules, episodesWatchedDays: v })}
            disabled={!!busy}
          />
          <label className="cleanup-keep">
            <TipLabel text={t('Tags are labels you put on titles. A movie or show with one of these tags is never touched by the cleanup rules.')}>{t('Always keep titles tagged')}</TipLabel>
            <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder={t('Keep, Kids')} disabled={!!busy} />
            <small className="hint">{t('Separate tags with commas. A tagged movie or show is never removed.')}</small>
          </label>
          <div className="row-actions" style={{ marginTop: 12 }}>
            <button
              className="btn-with-icon"
              disabled={!!busy || !anyRule}
              onClick={() =>
                void run('preview', async () => {
                  const rules = draft()
                  const p = await api.cleanupPreview(rules)
                  setPreview(p.items)
                  setPreviewRules(JSON.stringify(rules))
                })
              }
            >
              <Icon name="eye" size={15} /> {busy === 'preview' ? t('Checking…') : t('Preview')}
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
                  toast.success(t('Cleanup rules saved.'))
                })
              }
            >
              {t('Save rules')}
            </button>
            <Switch
              checked={rules.enabled}
              disabled={!!busy || !anyRule}
              onChange={(v) =>
                void run('enable', async () => {
                  if (v && !(await confirm({ title: t('Switch cleanup on?'), body: t('Once a day Mediarium deletes the files of titles that match these rules. Check the preview first.'), confirmLabel: t('Switch on'), danger: true })))
                    return
                  const s = await api.putWatchedSettings({ cleanup: { ...draft(), enabled: v } })
                  setSt(s)
                  setRules(s.cleanup)
                  toast.success(v ? t('Cleanup is on. It runs once a day.') : t('Cleanup is off.'))
                })
              }
              label={t('Run once a day')}
            />
          </div>
          {preview && (
            <div className="cleanup-preview">
              {preview.length === 0 ? (
                <p className="hint">{t('Nothing matches these rules right now.')}</p>
              ) : (
                <>
                  <p className="hint">
                    {plural(preview.length, 'One title matches. Nothing has been removed.', '{n} titles match. Nothing has been removed.')}
                    {preview.length > 50 ? ` ${t('A run removes up to 50 at a time.')}` : ''}
                  </p>
                  <ul>
                    {preview.slice(0, 100).map((it) => (
                      <li key={`${it.kind}-${it.id}`}>
                        <Icon name={it.kind === 'movie' ? 'film' : 'tv'} size={13} /> <strong>{it.title}</strong> <span className="hint">· {serverText(it.reason)}</span>
                      </li>
                    ))}
                  </ul>
                  {previewRules !== JSON.stringify(draft()) && <p className="hint">{t('The rules changed since this preview. Press Preview again before removing anything.')}</p>}
                  <button
                    className="btn-sm danger-ghost btn-with-icon"
                    disabled={!!busy || previewRules !== JSON.stringify(draft())}
                    onClick={() =>
                      void run('run', async () => {
                        if (!(await confirm({ title: t('Remove these now?'), body: t('The files of the titles listed are deleted now (into the recycle bin when it is on).'), confirmLabel: t('Remove now'), danger: true })))
                          return
                        await api.putWatchedSettings({ cleanup: JSON.parse(previewRules) })
                        const r = await api.runLibraryCleanup(preview)
                        toast.success(
                          r.failed.length
                            ? plural(r.removed.length, "Removed {n} title, {failed} couldn't be removed.", "Removed {n} titles, {failed} couldn't be removed.", { failed: r.failed.length })
                            : plural(r.removed.length, 'Removed {n} title.', 'Removed {n} titles.'),
                        )
                        setPreview(null)
                        load()
                      })
                    }
                  >
                    <Icon name="trash" size={14} /> {t('Remove these now')}
                  </button>
                </>
              )}
            </div>
          )}
          <p className="hint" style={{ marginBottom: 0 }}>
            {t('Last cleanup {when}.', { when: when(status.lastCleanup) })}
          </p>
        </fieldset>
      )}
    </>
  )
}
