import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, type MonitorStatus, type NotificationTarget, type NotifyType } from '../../api'
import Icon from '../../components/Icon'
import ServiceIcon from '../../components/ServiceIcon'
import { useToast } from '../../components/Toast'
import { timeAgo } from '../../format'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'

const EVENTS: { key: string; label: string; hint: string }[] = [
  { key: 'added', label: 'Download started', hint: 'A release was found and sent to the downloader' },
  { key: 'imported', label: 'Downloaded', hint: 'A download finished and was filed in your library' },
  { key: 'failed', label: 'Failed', hint: 'A download failed' },
  { key: 'conflict', label: 'Needs a decision', hint: 'A file already exists and Mediarium is asking what to do' },
  { key: 'subtitle', label: 'Subtitles', hint: 'A subtitle was downloaded' },
  { key: 'health', label: 'Something is wrong', hint: 'Your Usenet provider or an indexer stopped working, for example because a subscription expired' },
]

const FALLBACK_TYPES: NotifyType[] = [
  { type: 'discord', label: 'Discord', fields: [{ name: 'url', label: 'Webhook URL', kind: 'text', required: true, help: 'In Discord: channel settings, Integrations, Webhooks, New Webhook, Copy Webhook URL.' }] },
  { type: 'telegram', label: 'Telegram', fields: [{ name: 'botToken', label: 'Bot token', kind: 'password', required: true, help: 'Message @BotFather on Telegram, send /newbot and copy the token it gives you.' }, { name: 'chatId', label: 'Chat ID', kind: 'text', required: true, help: 'Send your bot a message, then open https://api.telegram.org/bot<token>/getUpdates to find the chat id.' }] },
  { type: 'webhook', label: 'Generic webhook', fields: [{ name: 'url', label: 'Webhook URL', kind: 'text', required: true, help: 'Mediarium sends a small JSON message to this address.' }] },
]

const TAGLINE: Record<string, string> = {
  email: 'Get an email from any SMTP account, such as Gmail or Outlook.',
  discord: 'Post to a channel in your Discord server.',
  telegram: 'Message yourself from a Telegram bot.',
  slack: 'Post to a Slack channel.',
  ntfy: 'Free phone push notifications. No account needed.',
  gotify: 'Push to your own Gotify server.',
  pushover: 'Push to your phone with the Pushover app.',
  webhook: 'Send JSON to any address, for your own automations.',
}

function summaryOf(t: NotificationTarget): string {
  const c = t.config ?? {}
  const bit = c.to || c.host || c.topic || c.chatId || c.url
  return bit ? String(bit).replace(/^https?:\/\//, '').slice(0, 42) : ''
}

interface Draft {
  uid: number
  type: string
  name: string
  values: Record<string, string>
  events: Set<string>
  open: boolean
  busy: boolean
}

// One notification service as a collapsible card: a draft you are filling in
// (not saved yet) or one that is already connected.
function Card({
  icon,
  title,
  sub,
  status,
  open,
  onToggle,
  onRemove,
  removeLabel,
  children,
}: {
  icon: string
  title: string
  sub: string
  status: 'draft' | 'saved'
  open: boolean
  onToggle: () => void
  onRemove: () => void
  removeLabel: string
  children: React.ReactNode
}) {
  return (
    <div className={`ncard ${status}${open ? ' open' : ''}`}>
      <div className="ncard-head">
        <button className="ncard-toggle" onClick={onToggle} aria-expanded={open}>
          <ServiceIcon type={icon} size={30} />
          <span className="ncard-text">
            <strong>{title}</strong>
            <small>{sub}</small>
          </span>
          <span className={`ncard-status ${status}`}>{status === 'draft' ? 'Not saved yet' : 'Connected'}</span>
          <span className="ncard-chev" style={{ transform: open ? 'rotate(90deg)' : 'none' }}>
            <Icon name="open" size={14} />
          </span>
        </button>
        <button className="ncard-remove" onClick={onRemove} title={removeLabel} aria-label={removeLabel}>
          <Icon name={status === 'draft' ? 'x' : 'trash'} size={16} />
        </button>
      </div>
      {open && <div className="ncard-body">{children}</div>}
    </div>
  )
}

function DraftBody({ d, def, update, onTest, onSave }: { d: Draft; def: NotifyType; update: (p: Partial<Draft>) => void; onTest: () => void; onSave: () => void }) {
  const valueOf = (f: NotifyType['fields'][number]) => d.values[f.name] ?? f.default ?? f.options?.[0]?.value ?? ''
  const missing = def.fields.some((f) => f.required && !valueOf(f).trim())
  return (
    <div className="grid-form">
      <div className="form-cols">
        <label>
          Name
          <input value={d.name} onChange={(e) => update({ name: e.target.value })} placeholder={def.label} />
        </label>
        {def.fields.map((f) => (
          <label key={f.name}>
            {f.label}
            {f.kind === 'select' ? (
              <select value={valueOf(f)} onChange={(e) => update({ values: { ...d.values, [f.name]: e.target.value } })}>
                {f.options?.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            ) : (
              <input
                type={f.kind === 'password' ? 'password' : f.kind === 'number' ? 'number' : 'text'}
                value={d.values[f.name] ?? f.default ?? ''}
                placeholder={f.placeholder}
                autoComplete="off"
                onChange={(e) => update({ values: { ...d.values, [f.name]: e.target.value } })}
              />
            )}
            {f.help && <small style={{ color: 'var(--text-dim)' }}>{f.help}</small>}
          </label>
        ))}
      </div>
      <div>
        <strong style={{ display: 'block', marginBottom: 8 }}>Tell me when</strong>
        <div className="event-grid">
          {EVENTS.map((e) => (
            <label key={e.key} className="check-row event-pick" title={e.hint}>
              <input
                type="checkbox"
                checked={d.events.has(e.key)}
                onChange={(ev) => {
                  const next = new Set(d.events)
                  if (ev.target.checked) next.add(e.key)
                  else next.delete(e.key)
                  update({ events: next })
                }}
              />
              <span>
                {e.label}
                <small>{e.hint}</small>
              </span>
            </label>
          ))}
        </div>
      </div>
      <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
        <button className="btn-with-icon" onClick={onTest} disabled={d.busy || missing}>
          <Icon name="play" size={14} /> Send a test
        </button>
        <button className="primary" onClick={onSave} disabled={d.busy || missing || d.events.size === 0}>
          Save
        </button>
      </div>
    </div>
  )
}

function Watch() {
  const toast = useToast()
  const [rows, setRows] = useState<MonitorStatus[] | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => {
    api.monitorStatus().then(setRows).catch(() => setRows([]))
  }, [])
  useEffect(load, [load])
  useLive(load, 30000)

  async function run() {
    setBusy(true)
    try {
      await api.runMonitor()
      toast.success('Checked your providers and indexers.')
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group usenet">
      <legend>
        <Icon name="activity" size={14} /> Connection watch
      </legend>
      <p style={{ marginTop: 0, color: 'var(--text-dim)' }}>
        Every half hour Mediarium checks that your Usenet provider and indexers still accept your login. If one stops working (an expired subscription, a changed password, the service being down) it notifies you once, and again when it recovers. Turn on <strong>Something is wrong</strong> on a notification to get these.
      </p>
      {rows && rows.length > 0 && (
        <ul className="watch-list">
          {rows.map((r) => (
            <li key={`${r.kind}-${r.id}`} className={r.ok ? 'ok' : 'bad'}>
              <Icon name={r.ok ? 'check' : 'warning'} size={16} />
              <div>
                <strong>{r.name}</strong> <small>{r.kind === 'usenet' ? 'Usenet provider' : 'Indexer'}</small>
                {!r.ok && <div className="watch-why">{r.hint || r.error}</div>}
              </div>
              <small>{timeAgo(r.checkedAt)}</small>
            </li>
          ))}
        </ul>
      )}
      {rows && rows.length === 0 && <p style={{ color: 'var(--text-dim)' }}>No results yet. Add a provider or indexer, then check now.</p>}
      <button className="btn-with-icon" onClick={run} disabled={busy}>
        <Icon name="refresh" size={14} /> {busy ? 'Checking…' : 'Check now'}
      </button>
    </fieldset>
  )
}

export default function NotificationSettings() {
  const confirm = useConfirm()
  const toast = useToast()
  const [types, setTypes] = useState<NotifyType[]>([])
  const [saved, setSaved] = useState<NotificationTarget[]>([])
  const [drafts, setDrafts] = useState<Draft[]>([])
  const [openSaved, setOpenSaved] = useState<Set<number>>(new Set())
  const uid = useRef(1)

  useEffect(() => {
    api.notificationTypes().then((t) => setTypes(t.length ? t : FALLBACK_TYPES)).catch(() => setTypes(FALLBACK_TYPES))
  }, [])
  const reload = useCallback(() => {
    api.listNotificationTargets().then(setSaved).catch(() => undefined)
  }, [])
  useEffect(reload, [reload])

  const ordered = useMemo(() => [...types].sort((a, b) => (a.type === 'email' ? -1 : b.type === 'email' ? 1 : 0)), [types])
  const labelOf = (t: string) => types.find((x) => x.type === t)?.label ?? t

  function addDraft(type: string) {
    const def = types.find((t) => t.type === type)
    setDrafts((d) => [
      {
        uid: uid.current++,
        type,
        name: def?.label ?? type,
        values: Object.fromEntries((def?.fields ?? []).filter((f) => f.default).map((f) => [f.name, f.default as string])),
        events: new Set(['imported', 'failed', 'health']),
        open: true,
        busy: false,
      },
      ...d,
    ])
  }
  const patch = (id: number, p: Partial<Draft>) => setDrafts((d) => d.map((x) => (x.uid === id ? { ...x, ...p } : x)))

  const bodyOf = (d: Draft) => {
    const def = types.find((t) => t.type === d.type)
    const selects = Object.fromEntries((def?.fields ?? []).filter((f) => f.kind === 'select' && f.options?.length).map((f) => [f.name, f.options![0].value]))
    return { name: d.name || def?.label, type: d.type, config: { ...selects, ...d.values }, events: [...d.events] }
  }

  async function testDraft(d: Draft) {
    patch(d.uid, { busy: true })
    try {
      const r = await api.testNotification(bodyOf(d))
      if (r.sent) toast.success('Test sent. Check your device.')
      else toast.error(r.error ?? 'The test did not go through.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      patch(d.uid, { busy: false })
    }
  }

  async function saveDraft(d: Draft) {
    patch(d.uid, { busy: true })
    try {
      await api.createNotificationTarget(bodyOf(d))
      toast.success(`${d.name || labelOf(d.type)} connected.`)
      setDrafts((all) => all.filter((x) => x.uid !== d.uid))
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      patch(d.uid, { busy: false })
    }
  }

  async function removeSaved(t: NotificationTarget) {
    if (!(await confirm({ title: `Remove "${t.name}"?`, body: <p>It stops receiving notifications.</p>, confirmLabel: 'Remove', danger: true }))) return
    await api.deleteNotificationTarget(t.id)
    toast.success(`${t.name} removed.`)
    reload()
  }

  async function testSaved(t: NotificationTarget) {
    try {
      const r = await api.testNotification({ id: t.id })
      if (r.sent) toast.success('Test sent. Check your device.')
      else toast.error(r.error ?? 'The test did not go through.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ margin: 0, color: 'var(--text-dim)' }}>
        Get told when something is added, finishes downloading, fails, or when a connection to your provider or an indexer breaks. Pick a service on the left to set it up, add as many as you like, and choose what each one hears about.
      </p>

      <div className="notif-cols span-all">
        <fieldset className="group alerts">
          <legend>
            <Icon name="plus" size={14} /> Add a way to be notified
          </legend>
          <div className="type-grid">
            {ordered.map((t) => (
              <button key={t.type} className="type-tile" onClick={() => addDraft(t.type)} title={`Set up ${t.label}`}>
                <ServiceIcon type={t.type} size={34} />
                <strong>{t.label}</strong>
                <small>{TAGLINE[t.type] ?? ''}</small>
              </button>
            ))}
          </div>
        </fieldset>

        <div className="notif-right">
          <fieldset className="group folders">
            <legend>
              <Icon name="check" size={14} /> Where notifications go
            </legend>
            {drafts.length === 0 && saved.length === 0 && <p style={{ margin: 0, color: 'var(--text-dim)' }}>Nothing set up yet. Pick a service on the left to add your first.</p>}
            <div className="ncard-stack">
              {drafts.map((d) => {
                const def = types.find((t) => t.type === d.type)
                if (!def) return null
                return (
                  <Card
                    key={d.uid}
                    icon={d.type}
                    title={d.name || def.label}
                    sub={def.label}
                    status="draft"
                    open={d.open}
                    onToggle={() => patch(d.uid, { open: !d.open })}
                    onRemove={() => setDrafts((all) => all.filter((x) => x.uid !== d.uid))}
                    removeLabel="Discard this one"
                  >
                    <DraftBody d={d} def={def} update={(p) => patch(d.uid, p)} onTest={() => void testDraft(d)} onSave={() => void saveDraft(d)} />
                  </Card>
                )
              })}
              {saved.map((t) => (
                <Card
                  key={t.id}
                  icon={t.type}
                  title={t.name}
                  sub={[labelOf(t.type), summaryOf(t)].filter(Boolean).join(' · ')}
                  status="saved"
                  open={openSaved.has(t.id)}
                  onToggle={() =>
                    setOpenSaved((s) => {
                      const n = new Set(s)
                      if (n.has(t.id)) n.delete(t.id)
                      else n.add(t.id)
                      return n
                    })
                  }
                  onRemove={() => void removeSaved(t)}
                  removeLabel="Remove"
                >
                  <div className="grid-form">
                    <div>
                      <strong style={{ display: 'block', marginBottom: 6 }}>Tells you when</strong>
                      <div className="target-events">
                        {(t.events ?? []).map((e) => (
                          <span key={e} className="badge">
                            {EVENTS.find((x) => x.key === e)?.label ?? e}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div>
                      <button className="btn-with-icon" onClick={() => void testSaved(t)}>
                        <Icon name="play" size={14} /> Send a test
                      </button>
                    </div>
                  </div>
                </Card>
              ))}
            </div>
          </fieldset>

          <Watch />
        </div>
      </div>
    </div>
  )
}
