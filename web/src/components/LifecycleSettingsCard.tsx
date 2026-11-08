import { useCallback, useEffect, useState } from 'react'
import { api, type LibraryCleanupItem, type LifecycleRules, type LifecycleState } from '../api'
import { plural, st as serverText, t } from '../i18n'
import { DaysRule, when } from './WatchedSettings'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import Switch from './Switch'
import TipLabel from './TipLabel'
import { useToast } from './Toast'

// The same tags in any order and case count as the same rules, like the server.
function ruleKey(r: LifecycleRules): string {
  const tags = r.keepTags.map((x) => x.trim().toLowerCase()).sort()
  return `${r.moviesWatchedDays}/${r.moviesUnwatchedDays}/${r.episodesWatchedDays}/${tags.join(',')}`
}

// Settings > Media types, under the cards while the library lifecycle module
// is on: the rules, a dry run that only lists what would go, and "Act for
// real", which can only be switched on after a dry run with these rules.
export default function LifecycleSettingsCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [st, setSt] = useState<LifecycleState | null>(null)
  const [rules, setRules] = useState<LifecycleRules | null>(null)
  const [tags, setTags] = useState('')
  const [busy, setBusy] = useState('')
  const [dry, setDry] = useState<{ items: LibraryCleanupItem[]; fresh: boolean } | null>(null)

  const take = useCallback((s: LifecycleState) => {
    setSt(s)
    setRules(s.rules)
    setTags(s.rules.keepTags.join(', '))
  }, [])
  const load = useCallback(() => {
    api
      .lifecycle()
      .then(take)
      .catch(() => setSt(null))
  }, [take])
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

  if (!st || !rules) return null
  const draft = (): LifecycleRules => ({ ...rules, keepTags: tags.split(',').map((x) => x.trim()).filter(Boolean) })
  const acting = st.rules.actForReal
  const anyRule = rules.moviesWatchedDays > 0 || rules.moviesUnwatchedDays > 0 || rules.episodesWatchedDays > 0
  const saved = ruleKey(st.rules) === ruleKey(draft())
  const changed = JSON.stringify(st.rules) !== JSON.stringify(draft())
  const canAct = anyRule && saved && st.dryRunDone
  const lock = !!busy || acting

  return (
    <section className="card lifecycle-card" style={{ ['--mc' as string]: 'var(--c-settings)' }}>
      <h2>{t('Library lifecycle')}</h2>
      <p style={{ color: 'var(--text-dim)' }}>{t('A title that matches a rule goes on the Leaving soon shelf. Its files are removed after the days below, unless someone keeps it.')}</p>
      {!st.watchFresh && <p className="error-text">{t("Needs what's been watched. Read it under Connections > Media servers.")}</p>}
      <DaysRule label={t('Movies watched a while ago')} hint={t('Remove a movie once it was last watched this many days ago.')} value={rules.moviesWatchedDays} onChange={(v) => setRules({ ...rules, moviesWatchedDays: v })} disabled={lock} />
      <DaysRule label={t('Movies nobody watched')} hint={t('Remove a movie that was never watched, this many days after it was added.')} value={rules.moviesUnwatchedDays} onChange={(v) => setRules({ ...rules, moviesUnwatchedDays: v })} disabled={lock} />
      <DaysRule label={t('Episodes watched a while ago')} hint={t('Remove an episode once it was last watched this many days ago.')} value={rules.episodesWatchedDays} onChange={(v) => setRules({ ...rules, episodesWatchedDays: v })} disabled={lock} />
      <div className="grid-form lifecycle-fields">
        <label>
          <TipLabel text={t('How long a title stays on the Leaving soon shelf before its files go. Titles already on the shelf keep their date.')}>{t('Days on the shelf first')}</TipLabel>
          <input
            type="number"
            min={1}
            max={365}
            value={rules.graceDays}
            disabled={!!busy}
            onChange={(e) => setRules({ ...rules, graceDays: Math.max(1, Math.min(365, Number(e.target.value) || 1)) })}
          />
        </label>
        <label>
          <TipLabel text={t('Tags are labels you put on titles. A movie or show with one of these tags is never touched by the cleanup rules.')}>{t('Always keep titles tagged')}</TipLabel>
          <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder={t('Keep, Kids')} disabled={lock} />
        </label>
      </div>
      {acting && <p className="hint">{t('Switch Act for real off to change the rules.')}</p>}
      <div className="row-actions lifecycle-actions">
        <button
          className="btn-with-icon"
          disabled={!!busy || acting || !anyRule}
          onClick={() =>
            void run('dry', async () => {
              take(await api.putLifecycle({ ...draft(), actForReal: false }))
              const r = await api.lifecycleDryRun()
              setDry({ items: r.items, fresh: r.watchFresh })
              load()
            })
          }
        >
          <Icon name="eye" size={15} /> {busy === 'dry' ? t('Checking…') : t('Dry run')}
        </button>
        <button
          className="primary btn-with-icon"
          disabled={!!busy || !changed}
          onClick={() =>
            void run('save', async () => {
              take(await api.putLifecycle({ ...draft(), actForReal: acting }))
              toast.success(t('Cleanup rules saved.'))
            })
          }
        >
          {t('Save rules')}
        </button>
        <Switch
          checked={acting}
          disabled={!!busy || (!acting && !canAct)}
          onChange={(v) =>
            void run('act', async () => {
              if (v && !(await confirm({ title: t('Act for real?'), body: t('Matching titles go on the Leaving soon shelf now. Their files are removed after the days on the shelf, unless someone keeps them.'), confirmLabel: t('Switch on'), danger: true }))) return
              take(await api.putLifecycle({ ...st.rules, actForReal: v }))
              toast.success(v ? t('Act for real is on.') : t('Act for real is off. The shelf is empty.'))
            })
          }
          label={t('Act for real')}
          description={canAct || acting ? t('Off, it only lists what would go.') : t('Run a dry run first.')}
        />
      </div>
      {dry && (
        <div className="cleanup-preview">
          {dry.items.length === 0 ? (
            <p className="hint">{t('Nothing matches these rules right now.')}</p>
          ) : (
            <>
              <p className="hint">{plural(dry.items.length, 'One title would go on the shelf. Nothing was removed.', '{n} titles would go on the shelf. Nothing was removed.')}</p>
              <ul>
                {dry.items.slice(0, 100).map((it) => (
                  <li key={`${it.kind}-${it.id}`}>
                    <Icon name={it.kind === 'movie' ? 'film' : 'tv'} size={13} /> <strong>{it.title}</strong> <span className="hint">· {serverText(it.reason)}</span>
                  </li>
                ))}
              </ul>
            </>
          )}
          {!dry.fresh && <p className="error-text">{t("Needs what's been watched. Read it under Connections > Media servers.")}</p>}
        </div>
      )}
      {st.keeps.length > 0 && (
        <div className="lifecycle-keeps">
          <h3>{t('Kept titles')}</h3>
          <ul>
            {st.keeps.map((k) => (
              <li key={`${k.kind}-${k.id}`}>
                <Icon name={k.kind === 'movie' ? 'film' : 'tv'} size={13} /> <strong>{k.title}</strong>
                <button className="btn-sm" disabled={!!busy} onClick={() => void run('unkeep', async () => take(await api.lifecycleUnkeep(k.kind, k.id)))}>
                  {t('Stop keeping')}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {acting && (
        <p className="hint">
          {t('Last run {when}.', { when: when(st.status.lastRun) })}
        </p>
      )}
    </section>
  )
}
