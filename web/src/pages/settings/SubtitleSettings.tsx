import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../../api'
import ServiceKeyCard from '../../components/ServiceKeyCard'
import { OPENSUBTITLES_COPY } from '../../components/serviceCopy'
import Switch from '../../components/Switch'
import TestButton from '../../components/TestButton'
import { useToast } from '../../components/Toast'
import { SUBTITLE_LANGUAGES } from '../../languages'
import { useModules } from '../../ModulesContext'
import { useAutosaveSetting } from '../../useAutosave'
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { firstError, maxLength, required } from '../../validate'

function SubtitlesSection() {
  const [languages, setLanguages] = useState<string[]>(['en'])
  const [auto, setAuto] = useState(false)
  const [upgrade, setUpgrade] = useState(true)
  const [hasKey, setHasKey] = useState(false)
  const [status, setStatus] = useState('')
  const [sweeping, setSweeping] = useState(false)
  // Saving before the current choices have arrived would replace them with the defaults.
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const toast = useToast()
  const v = useValidation({ languages: languages.length === 0 ? 'Pick at least one language to keep subtitles for.' : null })

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setHasKey(!!s.hasOpenSubtitlesApiKey)
        setLanguages(s.subtitleLanguages?.length ? s.subtitleLanguages : ['en'])
        setAuto(s.subtitleAutoDownload ?? false)
        setUpgrade(s.subtitleUpgrade ?? true)
        setLoaded(true)
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [])

  function toggleLanguage(code: string) {
    setLanguages((cur) => (cur.includes(code) ? cur.filter((c) => c !== code) : [...cur, code]))
  }

  async function save() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      await api.putSettings({ subtitleLanguages: languages, subtitleAutoDownload: auto, subtitleUpgrade: upgrade })
      toast.success('Saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  async function sweep() {
    setSweeping(true)
    setStatus('Searching for missing subtitles…')
    try {
      setStatus((await api.subtitleSweep()).message)
    } catch (e) {
      setStatus(e instanceof Error ? e.message : String(e))
    } finally {
      setSweeping(false)
    }
  }

  return (
    <section className="card grid-form">
      <h2>Languages</h2>
      <p style={{ color: 'var(--text-dim)' }}>Subtitles are saved next to each movie and episode, in the languages you pick.</p>
      <fieldset style={{ border: '1px solid var(--border)', borderRadius: 'var(--radius)', padding: 12 }}>
        <legend>Languages to keep</legend>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(190px, 1fr))', gap: 6 }}>
          {SUBTITLE_LANGUAGES.map((l) => (
            <label key={l.code} style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
              <input type="checkbox" checked={languages.includes(l.code)} onChange={() => toggleLanguage(l.code)} />
              {l.name}
            </label>
          ))}
        </div>
        <FieldError v={v} name="languages" />
      </fieldset>
      <label style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
        <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
        Download after each import, and look again every few hours for anything missing
      </label>
      <label style={{ display: 'flex', gap: 6, alignItems: 'center', marginLeft: 22, opacity: auto ? 1 : 0.55 }}>
        <input type="checkbox" checked={upgrade} disabled={!auto} onChange={(e) => setUpgrade(e.target.checked)} />
        For a month after, swap a subtitle for one made for that exact video file if one turns up
      </label>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button className="primary" onClick={save} disabled={!loaded || saving}>
          Save languages
        </button>
        <button disabled={!hasKey || sweeping} onClick={sweep}>
          Search for missing subtitles now
        </button>
      </div>
      {status && <span style={{ color: 'var(--text-dim)' }}>{status}</span>}
      {loadError && <p className="error-text">Could not load your current choices: {loadError}</p>}
    </section>
  )
}

function OpenSubtitlesCard() {
  const [s, setS] = useState<SettingsData | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const toast = useToast()
  // Blank username and password together mean "no account". A password on its own is a mistake.
  const v = useValidation({
    username: firstError(password ? required(username, 'Add your OpenSubtitles username to go with this password.') : null, maxLength(username, 100, 'The username')),
  })

  const load = useCallback(() => {
    api
      .getSettings()
      .then((data) => {
        setS(data)
        setUsername(data.openSubtitlesAccountName ?? '')
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function saveAccount() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      await api.putSettings({ openSubtitlesUsername: username.trim(), openSubtitlesPassword: password })
      setPassword('')
      toast.success(username.trim() ? 'OpenSubtitles account saved.' : 'OpenSubtitles account removed.')
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!s) return loadError ? <p className="error-text">{loadError}</p> : <p>Loading…</p>
  return (
    <ServiceKeyCard
      service="opensubtitles"
      {...OPENSUBTITLES_COPY}
      builtIn={!!s.openSubtitlesKeyBuiltIn}
      configured={!!s.hasOpenSubtitlesApiKey}
      onSaved={load}
      allowOverride
      usingOwnKey={!!s.openSubtitlesUsingOwnKey}
    >
      <div className="grid-form" style={{ marginTop: 18, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
        <h3 style={{ margin: 0 }}>Your OpenSubtitles.com account (optional)</h3>
        <p style={{ color: 'var(--text-dim)', margin: 0 }}>A free account raises the daily limit from about 5 downloads to about 20.</p>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" {...v.bind('username', username, setUsername)} />
          <FieldError v={v} name="username" />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={s.hasOpenSubtitlesAccount ? 'leave blank to keep the saved password' : ''}
            autoComplete="new-password"
          />
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" onClick={saveAccount} disabled={saving || (!username.trim() && !s.hasOpenSubtitlesAccount)}>
            {username.trim() ? 'Save account' : 'Remove account'}
          </button>
          <TestButton
            label="Test login"
            disabled={!s.hasOpenSubtitlesApiKey}
            run={async () => {
              if (!username.trim() && password) return { ok: false, message: 'Add your OpenSubtitles username to go with this password.' }
              return api.testService({ service: 'opensubtitles', username: username.trim(), password })
            }}
          />
        </div>
        <FormProblem v={v} />
      </div>
    </ServiceKeyCard>
  )
}

// The master switch, at the top.
function SubtitlesSwitch({ enabled, onChange, disabled }: { enabled: boolean; onChange: (next: boolean) => void; disabled: boolean }) {
  return (
    <section className="card span-all">
      <Switch
        checked={enabled}
        onChange={onChange}
        disabled={disabled}
        label="Subtitles for my movies and shows"
        description="Finds and saves subtitles next to each movie and episode."
        showState
      />
    </section>
  )
}

export default function SubtitleSettings() {
  const { refresh } = useModules()
  const sw = useAutosaveSetting<boolean>(
    (s) => s.subtitlesEnabled ?? false,
    (v) => ({ subtitlesEnabled: v }),
    (saved) => (saved ? 'Saved: subtitles are on.' : 'Saved: subtitles are off.'),
    false,
  )
  async function flip(next: boolean) {
    await sw.change(next)
    void refresh() // the rest of the app shows or hides its subtitle parts
  }

  const details = (
    <div className="settings-stack span-all">
      <OpenSubtitlesCard />
      <SubtitlesSection />
    </div>
  )
  return (
    <div className="settings-stack">
      <SubtitlesSwitch enabled={sw.value} onChange={(v) => void flip(v)} disabled={!sw.loaded || sw.saving} />
      {sw.loaded &&
        (sw.value ? (
          details
        ) : (
          <details className="card files-card span-all">
            <summary>
              <h2 style={{ margin: 0, display: 'inline' }}>Account, languages and automatic downloads</h2>
            </summary>
            <p style={{ color: 'var(--text-dim)' }}>Turn subtitles on to change these.</p>
            <fieldset disabled className="plain-fieldset">
              {details}
            </fieldset>
          </details>
        ))}
    </div>
  )
}
