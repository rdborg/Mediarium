import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type MonitorStatus, type NotificationTarget, type NotifyField, type NotifyTestStep, type NotifyType } from '../../api'
import Icon from '../../components/Icon'
import PublicAddress from '../../components/PublicAddress'
import QuietHours from '../../components/QuietHours'
import ServiceIcon from '../../components/ServiceIcon'
import TerminalLog from '../../components/TerminalLog'
import { useToast } from '../../components/Toast'
import { timeAgo } from '../../format'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'
import * as check from '../../validate'
import { FieldError, FormProblem, useValidation, type Validation } from '../../useValidation'

const EVENTS: { key: string; label: string; hint: string }[] = [
  { key: 'added', label: 'Download started', hint: 'A release was found and sent to the downloader' },
  { key: 'imported', label: 'Downloaded', hint: 'A download finished and was filed in your library' },
  { key: 'failed', label: 'Failed', hint: 'A download failed' },
  { key: 'conflict', label: 'Needs a decision', hint: 'A file already exists and Mediarium is asking what to do' },
  { key: 'subtitle', label: 'Subtitles', hint: 'A subtitle was downloaded' },
  { key: 'health', label: 'Something is wrong', hint: 'Your Usenet provider or an indexer stopped working, for example because a subscription expired' },
  { key: 'update', label: 'New version', hint: 'A new version of Mediarium is available' },
]

const FALLBACK_TYPES: NotifyType[] = [
  { type: 'discord', label: 'Discord', fields: [{ name: 'url', label: 'Webhook URL', kind: 'text', required: true, help: 'In Discord: channel settings, Integrations, Webhooks, New Webhook, Copy Webhook URL.' }] },
  { type: 'telegram', label: 'Telegram', fields: [{ name: 'botToken', label: 'Bot token', kind: 'password', required: true, help: 'Message @BotFather on Telegram, send /newbot and copy the token it gives you.' }, { name: 'chatId', label: 'Chat ID', kind: 'text', required: true, help: 'Send your bot a message, then open https://api.telegram.org/bot<token>/getUpdates to find the chat ID.' }] },
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

const DEFAULT_EVENTS = ['imported', 'failed', 'health', 'update']

// What one form holds while you fill it in: not saved anywhere until you press
// Add (or Save changes).
interface FormState {
  name: string
  values: Record<string, string>
  events: Set<string>
}

interface TestNote {
  ok: boolean
  text: string
  steps?: NotifyTestStep[]
}

function summaryOf(t: NotificationTarget): string {
  const c = t.config ?? {}
  const bit = c.to || c.host || c.topic || c.chatId || c.url
  return bit ? String(bit).replace(/^https?:\/\//, '').slice(0, 42) : ''
}

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

function freshForm(def: NotifyType): FormState {
  return {
    name: '',
    values: Object.fromEntries((def.fields ?? []).filter((f) => f.default).map((f) => [f.name, f.default as string])),
    events: new Set(DEFAULT_EVENTS),
  }
}

function valueOf(f: NotifyField, s: FormState): string {
  return s.values[f.name] ?? f.default ?? f.options?.[0]?.value ?? ''
}

// What to say when a required field is left empty, for the methods where a
// generic sentence would not be much help.
const NEEDED: Record<string, string> = {
  'email.host': "Add your email provider's outgoing (SMTP) server, for example smtp.gmail.com.",
  'email.port': 'Add the port. It is usually 587.',
  'email.from': 'Add the address the emails should come from, for example you@example.com.',
  'email.to': 'Add the address the notifications should be sent to, for example you@example.com.',
  'ntfy.topic': 'Add a topic name, for example mediarium-a1b2c3.',
  'gotify.url': 'Add the address of your Gotify server, for example https://gotify.example.com.',
  'gotify.token': 'Add the app token from your Gotify server.',
  'pushover.userKey': 'Add your Pushover user key.',
  'pushover.token': 'Add the app token from Pushover.',
  'slack.url': 'Add the Slack webhook URL. It starts with https://hooks.slack.com/.',
  'discord.url': 'Add the Discord webhook URL. It starts with https://discord.com/api/webhooks/.',
  'telegram.botToken': 'Add the bot token that @BotFather gave you.',
  'telegram.chatId': 'Add the chat ID, a number like 123456789.',
  'webhook.url': 'Add the address to send the messages to, for example https://example.com/hook.',
}

const MAILBOX_HINT = 'It should look like you@example.com or Mediarium <you@example.com>.'

// One email address, either bare or written as: Name <name@example.com>.
function mailbox(value: string): boolean {
  const m = /^(.*)<([^<>]*)>$/.exec(value.trim())
  return check.email(m ? m[2].trim() : value.trim()) === null
}

function fieldProblem(type: string, f: NotifyField, s: FormState, stored: boolean): check.Message {
  const raw = valueOf(f, s)
  const v = raw.trim()
  if (!v) {
    if (stored) return null
    return f.required ? (NEEDED[`${type}.${f.name}`] ?? `Fill in the ${f.label.toLowerCase()}.`) : null
  }
  switch (f.name) {
    case 'host':
      return check.hostOrIP(v, 'smtp.gmail.com')
    case 'port':
      return check.port(v)
    case 'from':
      return mailbox(v) ? null : `That doesn't look like an email address. ${MAILBOX_HINT}`
    case 'to': {
      const bad = v.split(/[,;]/).map((a) => a.trim()).filter((a) => a && !mailbox(a))
      if (bad.length > 0) return `"${bad[0]}" doesn't look like an email address. It should look like you@example.com. Separate several addresses with commas.`
      return v.split(/[,;]/).some((a) => a.trim()) ? null : `Add at least one email address, for example you@example.com.`
    }
    case 'server':
      return check.url(v, { requireScheme: true, example: 'https://ntfy.sh' })
    case 'url':
      return check.url(v, {
        requireScheme: true,
        example: type === 'gotify' ? 'https://gotify.example.com' : type === 'slack' ? 'https://hooks.slack.com/services/...' : type === 'discord' ? 'https://discord.com/api/webhooks/...' : 'https://example.com/hook',
      })
    case 'topic':
      return /[\s/?#]/.test(v) ? "A topic can't contain spaces, slashes, question marks or #. Use letters, numbers, dashes and underscores." : null
    case 'chatId':
      return /^-?\d+$/.test(v) || /^@\w+$/.test(v) ? null : 'The chat ID is a number like 123456789 (a group has a minus sign in front, like -1001234567890). A channel name such as @mychannel also works.'
    case 'template': {
      // The same placeholders Mediarium fills in are swapped for sample text, then the result must be valid JSON.
      try {
        JSON.parse(v.replace(/\{\{(event|title|message|at)\}\}/g, 'x'))
        return null
      } catch {
        return 'That is not valid JSON. Check the brackets, quotes and commas. You can use {{event}}, {{title}}, {{message}} and {{at}} inside quotes.'
      }
    }
  }
  if (f.kind === 'number') return check.numberRange(v, f.label, { min: 1, max: 65535 })
  if (f.kind === 'password') return check.apiKey(v)
  return null
}

// Every problem in one method's form, keyed by field name.
function formProblems(def: NotifyType, s: FormState, stored?: Record<string, boolean>): Record<string, check.Message> {
  const out: Record<string, check.Message> = { name: check.maxLength(s.name, 100, 'The name') }
  for (const f of def.fields) out[f.name] = fieldProblem(def.type, f, s, !!stored?.[f.name])
  if (def.type === 'email') {
    // A login needs both halves: a username and its password.
    const hasUser = (s.values.username ?? '').trim() !== ''
    const hasPass = (s.values.password ?? '').trim() !== '' || !!stored?.password
    if (hasUser && !hasPass && !out.password) out.password = 'Add the password for this username, or clear the username if your server needs no login.'
    if (hasPass && !hasUser && !out.username) out.username = 'Add the username that goes with this password, or clear the password if your server needs no login.'
  }
  out.events = s.events.size === 0 ? 'Choose at least one thing to be told about.' : null
  return out
}

function bodyOf(def: NotifyType, s: FormState) {
  const selects = Object.fromEntries((def.fields ?? []).filter((f) => f.kind === 'select' && f.options?.length).map((f) => [f.name, f.options![0].value]))
  return { name: s.name.trim() || def.label, type: def.type, config: { ...selects, ...s.values }, events: [...s.events] }
}

// The connection form for one method: the fields, live checks, and what to tell you about.
function MethodForm({
  def,
  state,
  onChange,
  stored,
  children,
}: {
  def: NotifyType
  state: FormState
  onChange: (next: FormState) => void
  stored?: Record<string, boolean>
  children: (v: Validation) => React.ReactNode
}) {
  const v = useValidation(formProblems(def, state, stored))
  const setValue = (name: string) => (text: string) => onChange({ ...state, values: { ...state.values, [name]: text } })
  return (
    <div className="method-form">
      <div className="method-fields">
        <label>
          Name
          <input value={state.name} onChange={(e) => onChange({ ...state, name: e.target.value })} placeholder={def.label} {...v.bind('name', state.name, (t) => onChange({ ...state, name: t }))} />
          <FieldError v={v} name="name" />
          <small className="field-help">Only for you, to tell your notifications apart.</small>
        </label>
        {def.fields.map((f) => {
          const isStored = !!stored?.[f.name]
          const text = state.values[f.name] ?? f.default ?? ''
          return (
            <label key={f.name}>
              <span>
                {f.label}
                {f.required && !isStored && <span className="req-star" aria-hidden="true"> *</span>}
              </span>
              {f.kind === 'select' ? (
                <select value={valueOf(f, state)} onChange={(e) => setValue(f.name)(e.target.value)} {...v.bind(f.name)}>
                  {f.options?.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  type={f.kind === 'password' ? 'password' : 'text'}
                  inputMode={f.kind === 'number' ? 'numeric' : undefined}
                  value={text}
                  placeholder={isStored ? 'Saved. Leave empty to keep it.' : f.placeholder}
                  autoComplete="off"
                  onChange={(e) => setValue(f.name)(e.target.value)}
                  {...v.bind(f.name, text, f.kind === 'password' ? undefined : setValue(f.name))}
                />
              )}
              <FieldError v={v} name={f.name} />
              {f.help && <small className="field-help">{f.help}</small>}
            </label>
          )
        })}
      </div>
      <div>
        <strong className="method-sub">Tell me when</strong>
        <div className="event-grid">
          {EVENTS.map((e) => (
            <label key={e.key} className="check-row event-pick" title={e.hint}>
              <input
                type="checkbox"
                checked={state.events.has(e.key)}
                onChange={(ev) => {
                  const next = new Set(state.events)
                  if (ev.target.checked) next.add(e.key)
                  else next.delete(e.key)
                  onChange({ ...state, events: next })
                }}
              />
              <span>
                {e.label}
                <small>{e.hint}</small>
              </span>
            </label>
          ))}
        </div>
        <FieldError v={v} name="events" />
      </div>
      {children(v)}
    </div>
  )
}

// The log under the form: what the test did, line by line, and how it ended.
function TestLog({ note, running }: { note: TestNote | null; running: boolean }) {
  if (!running && !note?.steps?.length) return null
  return <TerminalLog lines={running ? [] : (note?.steps ?? [])} running={running} label="Test log" />
}

function TestLine({ note }: { note: TestNote | null }) {
  if (!note) return null
  return (
    <div className={`indexer-result ${note.ok ? 'ok' : 'bad'}`} role="status">
      <Icon name={note.ok ? 'check' : 'warning'} size={14} /> {note.text}
    </div>
  )
}

// One notification you have already set up.
function TargetRow({
  t,
  def,
  onChanged,
}: {
  t: NotificationTarget
  def: NotifyType | undefined
  onChanged: () => void
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState('')
  const [note, setNote] = useState<TestNote | null>(null)
  const [form, setForm] = useState<FormState>({ name: t.name, values: { ...(t.config ?? {}) }, events: new Set(t.events ?? []) })
  const label = def?.label ?? t.type

  function openEditor() {
    setForm({ name: t.name, values: { ...(t.config ?? {}) }, events: new Set(t.events ?? []) })
    setNote(null)
    setEditing(true)
  }

  async function run(what: string, fn: () => Promise<void>) {
    setBusy(what)
    try {
      await fn()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy('')
    }
  }

  const sendTest = (data: Record<string, unknown>) =>
    run('test', async () => {
      const r = await api.testNotification(data)
      setNote(r.sent ? { ok: true, text: 'Test sent. Check your device.', steps: r.steps } : { ok: false, text: r.error ?? 'The test did not go through.', steps: r.steps })
      if (r.sent) toast.success('Test sent. Check your device.')
      else toast.error(r.error ?? 'The test did not go through.')
    })

  const toggle = (on: boolean) =>
    run('toggle', async () => {
      await api.updateNotificationTarget(t.id, { name: t.name, type: t.type, enabled: on, events: t.events, config: t.config })
      toast.success(on ? `${t.name} is on.` : `${t.name} is off. It sends nothing until you switch it on.`)
      onChanged()
    })

  return (
    <div className={`ntarget${t.enabled ? '' : ' is-off'}${editing ? ' editing' : ''}`}>
      <div className="ntarget-main">
        <ServiceIcon type={t.type} size={38} />
        <div className="ntarget-text">
          <strong>{t.name}</strong>
          <small>{[label, summaryOf(t)].filter(Boolean).join(' · ')}</small>
          <div className="target-events">
            {(t.events ?? []).map((e) => (
              <span key={e} className="badge" title={EVENTS.find((x) => x.key === e)?.hint}>
                {EVENTS.find((x) => x.key === e)?.label ?? e}
              </span>
            ))}
          </div>
        </div>
        <label className="ntarget-switch" title={t.enabled ? 'On. Press to switch off.' : 'Off. Press to switch on.'}>
          <span className="switch">
            <input type="checkbox" role="switch" aria-label={`${t.name} is ${t.enabled ? 'on' : 'off'}`} checked={t.enabled} disabled={!!busy} onChange={(e) => void toggle(e.target.checked)} />
            <span className="switch-track" aria-hidden="true">
              <span className="switch-thumb" />
            </span>
          </span>
          <span className="ntarget-state">{t.enabled ? 'On' : 'Off'}</span>
        </label>
      </div>
      <div className="ntarget-actions">
        <button className="btn-sm btn-with-icon" disabled={!!busy} onClick={() => void sendTest({ id: t.id })}>
          <Icon name="play" size={13} /> {busy === 'test' && !editing ? 'Sending…' : 'Test'}
        </button>
        <button className="btn-sm btn-with-icon" onClick={() => (editing ? setEditing(false) : openEditor())} aria-expanded={editing}>
          <Icon name={editing ? 'x' : 'sliders'} size={13} /> {editing ? 'Close' : 'Edit'}
        </button>
        <button
          className="btn-sm btn-danger btn-with-icon"
          disabled={!!busy}
          onClick={async () => {
            if (!(await confirm({ title: `Remove "${t.name}"?`, body: <p>It stops receiving notifications.</p>, confirmLabel: 'Remove', danger: true }))) return
            await run('remove', async () => {
              await api.deleteNotificationTarget(t.id)
              toast.success(`${t.name} removed.`)
              onChanged()
            })
          }}
        >
          <Icon name="trash" size={13} /> Remove
        </button>
      </div>
      {!editing && <TestLine note={note} />}
      {!editing && <TestLog note={note} running={busy === 'test'} />}
      {editing && def && (
        <div className="ntarget-edit">
          <MethodForm def={def} state={form} onChange={(f) => { setForm(f); setNote(null) }} stored={t.hasSecrets}>
            {(v) => (
              <>
                <TestLine note={note} />
                <div className="form-actions">
                  <button className="btn-with-icon" disabled={!!busy} onClick={() => v.attempt() && void sendTest({ ...bodyOf(def, form), id: t.id })}>
                    <Icon name="play" size={14} /> {busy === 'test' ? 'Sending…' : 'Send a test'}
                  </button>
                  <button
                    className="primary"
                    disabled={!!busy}
                    onClick={() => {
                      if (!v.attempt()) return
                      void run('save', async () => {
                        await api.updateNotificationTarget(t.id, { ...bodyOf(def, form), enabled: t.enabled })
                        toast.success(`${form.name.trim() || t.name} saved.`)
                        setEditing(false)
                        onChanged()
                      })
                    }}
                  >
                    {busy === 'save' ? 'Saving…' : 'Save changes'}
                  </button>
                  <button onClick={() => setEditing(false)}>Cancel</button>
                </div>
                <TestLog note={note} running={busy === 'test'} />
                <FormProblem v={v} verb="save" />
              </>
            )}
          </MethodForm>
        </div>
      )}
    </div>
  )
}

function Watch() {
  const toast = useToast()
  const [rows, setRows] = useState<MonitorStatus[] | null>(null)
  const [busy, setBusy] = useState(false)
  // How often the watch runs, in minutes. 0 is off, and anything else is at least 5.
  const [minutes, setMinutes] = useState<number | null>(null)
  const [minutesText, setMinutesText] = useState('')
  const [savingMinutes, setSavingMinutes] = useState(false)
  const minutesError = check.firstError(
    minutesText.trim() === '' && 'Enter a number of minutes, or 0 to turn the checks off.',
    check.numberRange(minutesText, 'The number of minutes', { min: 0, max: 10080 }),
    minutesText.trim() !== '' && Number(minutesText) > 0 && Number(minutesText) < 5 && 'The checks can run every 5 minutes at the most. Use 5 or more, or 0 to turn them off.',
  )
  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        const n = s.monitorIntervalMinutes ?? 30
        setMinutes(n)
        setMinutesText(String(n))
      })
      .catch(() => undefined)
  }, [])

  async function saveMinutes() {
    if (minutesError) return
    setSavingMinutes(true)
    try {
      const saved = await api.putSettings({ monitorIntervalMinutes: Number(minutesText) })
      const n = saved.monitorIntervalMinutes ?? Number(minutesText)
      setMinutes(n)
      setMinutesText(String(n))
      toast.success(n === 0 ? 'Saved: the connection watch is off.' : `Saved: checking every ${n} minutes.`)
    } catch (e) {
      toast.error(`Not saved: ${errText(e)}`)
    } finally {
      setSavingMinutes(false)
    }
  }
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
      toast.error(errText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group usenet span-all">
      <legend>
        <Icon name="activity" size={14} /> Connection watch
      </legend>
      <p style={{ marginTop: 0, color: 'var(--text-dim)' }}>
        {minutes === 0
          ? 'The checks are off, so nothing tells you when your Usenet provider or an indexer stops working. Turn them back on below. '
          : `Every ${minutes === null || minutes === 30 ? 'half hour' : minutes === 60 ? 'hour' : `${minutes} minutes`} Mediarium checks that your Usenet provider and indexers still accept your login. `}
        {minutes !== 0 && (
          <>
            If one stops working (an expired subscription, a changed password, the service being down) it notifies you once, and again when it recovers. Turn on <strong>Something is wrong</strong> on a notification to get these.
          </>
        )}
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
      <div style={{ marginTop: 12 }}>
        <label style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
          Check every
          <input
            inputMode="numeric"
            value={minutesText}
            onChange={(e) => setMinutesText(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && minutesText.trim() !== String(minutes) && void saveMinutes()}
            aria-invalid={minutesError ? true : undefined}
            disabled={minutes === null}
            style={{ width: 90 }}
          />
          minutes
          <button onClick={() => void saveMinutes()} disabled={savingMinutes || minutes === null || minutesText.trim() === String(minutes) || !!minutesError}>
            {savingMinutes ? 'Saving…' : 'Save'}
          </button>
        </label>
        <small style={{ color: minutesError ? 'var(--danger)' : 'var(--text-dim)' }}>{minutesError ?? 'Use 0 to turn the checks off. The lowest is 5 minutes.'}</small>
      </div>
    </fieldset>
  )
}

// Settings > Connections > Notifications: the ones you have set up on top, and
// below them a list of every method to choose from with the form for it.
export default function NotificationSettings() {
  const toast = useToast()
  const [types, setTypes] = useState<NotifyType[]>([])
  const [saved, setSaved] = useState<NotificationTarget[] | null>(null)
  const [picked, setPicked] = useState('')
  // What you typed in each method, kept while you look at the others.
  const [forms, setForms] = useState<Record<string, FormState>>({})
  const [notes, setNotes] = useState<Record<string, TestNote | null>>({})
  const [busy, setBusy] = useState('')
  // Bumped after a method is added, so its form starts clean.
  const [resets, setResets] = useState<Record<string, number>>({})

  useEffect(() => {
    api.notificationTypes().then((t) => setTypes(t.length ? t : FALLBACK_TYPES)).catch(() => setTypes(FALLBACK_TYPES))
  }, [])
  const reload = useCallback(() => {
    api.listNotificationTargets().then(setSaved).catch(() => setSaved([]))
  }, [])
  useEffect(reload, [reload])

  const ordered = useMemo(() => [...types].sort((a, b) => (a.type === 'email' ? -1 : b.type === 'email' ? 1 : 0)), [types])
  const defOf = (t: string) => types.find((x) => x.type === t)
  const current = defOf(picked) ?? ordered[0]
  const formOf = (def: NotifyType): FormState => forms[def.type] ?? freshForm(def)
  const started = (def: NotifyType) => {
    const f = forms[def.type]
    return !!f && (f.name.trim() !== '' || Object.entries(f.values).some(([k, v]) => v !== '' && v !== def.fields.find((x) => x.name === k)?.default))
  }

  function setForm(def: NotifyType, next: FormState) {
    setForms((all) => ({ ...all, [def.type]: next }))
    setNotes((n) => ({ ...n, [def.type]: null }))
  }

  async function test(def: NotifyType) {
    setBusy('test')
    try {
      const r = await api.testNotification(bodyOf(def, formOf(def)))
      setNotes((n) => ({ ...n, [def.type]: r.sent ? { ok: true, text: 'Test sent. Check your device, then press Add.', steps: r.steps } : { ok: false, text: r.error ?? 'The test did not go through.', steps: r.steps } }))
    } catch (e) {
      setNotes((n) => ({ ...n, [def.type]: { ok: false, text: errText(e) } }))
    } finally {
      setBusy('')
    }
  }

  async function add(def: NotifyType) {
    setBusy('add')
    try {
      const form = formOf(def)
      await api.createNotificationTarget(bodyOf(def, form))
      toast.success(`${form.name.trim() || def.label} added.`)
      setForms((all) => {
        const rest = { ...all }
        delete rest[def.type]
        return rest
      })
      setNotes((n) => ({ ...n, [def.type]: null }))
      setResets((r) => ({ ...r, [def.type]: (r[def.type] ?? 0) + 1 }))
      reload()
    } catch (e) {
      toast.error(errText(e))
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ margin: 0, color: 'var(--text-dim)' }}>
        Get a message when a download starts, finishes or fails, or when the connection to your provider or an indexer breaks. Add as many notifications as you like and choose what each one tells you about.
      </p>

      <fieldset className="group folders span-all">
        <legend>
          <Icon name="mail" size={14} /> Your notifications {saved && <small>({saved.length})</small>}
        </legend>
        {saved === null && <div className="skeleton" style={{ height: 80 }} />}
        {saved && saved.length === 0 && (
          <div className="method-empty">
            <Icon name="mail" size={28} />
            <div>
              <strong>Nothing has been set up to notify you yet.</strong>
              <p>Pick a way to be notified below. <strong>ntfy</strong> is the quickest: free push messages to your phone, no account needed.</p>
            </div>
          </div>
        )}
        {saved && saved.length > 0 && (
          <div className="ntarget-list">
            {saved.map((t) => (
              <TargetRow key={t.id} t={t} def={defOf(t.type)} onChanged={reload} />
            ))}
          </div>
        )}
      </fieldset>

      <fieldset className="group alerts span-all">
        <legend>
          <Icon name="plus" size={14} /> Add a notification
        </legend>
        {current && (
          <div className="method-add">
            <div className="method-list" role="tablist" aria-label="Ways to be notified">
              {ordered.map((t) => (
                <button
                  key={t.type}
                  role="tab"
                  aria-selected={current.type === t.type}
                  className={`method-item${current.type === t.type ? ' active' : ''}`}
                  style={{ ['--mc' as string]: 'var(--c-wanted)' }}
                  onClick={() => setPicked(t.type)}
                >
                  <ServiceIcon type={t.type} size={30} />
                  <span className="mi-text">
                    <strong>{t.label}</strong>
                    <small>{TAGLINE[t.type] ?? ''}</small>
                  </span>
                  {started(t) && <span className="mi-dot" title="You have started filling this in" />}
                </button>
              ))}
            </div>
            <div className="method-panel" role="tabpanel" style={{ ['--mc' as string]: 'var(--c-wanted)' }}>
              <div className="method-head">
                <ServiceIcon type={current.type} size={46} />
                <div>
                  <h3>{current.label}</h3>
                  <p>{TAGLINE[current.type] ?? 'Fill in the details, send a test, then add it.'}</p>
                </div>
              </div>
              <MethodForm key={`${current.type}-${resets[current.type] ?? 0}`} def={current} state={formOf(current)} onChange={(f) => setForm(current, f)}>
                {(v) => (
                  <>
                    <TestLine note={notes[current.type] ?? null} />
                    <div className="form-actions">
                      <button className="btn-with-icon" onClick={() => v.attempt() && void test(current)} disabled={!!busy}>
                        <Icon name="play" size={14} /> {busy === 'test' ? 'Sending…' : 'Send a test'}
                      </button>
                      <button className="primary" onClick={() => v.attempt() && void add(current)} disabled={!!busy}>
                        {busy === 'add' ? 'Adding…' : `Add ${current.label}`}
                      </button>
                    </div>
                    <TestLog note={notes[current.type] ?? null} running={busy === 'test'} />
                    <FormProblem v={v} verb="add this notification" />
                  </>
                )}
              </MethodForm>
            </div>
          </div>
        )}
      </fieldset>

      <QuietHours />

      <PublicAddress />

      <Watch />
    </div>
  )
}
