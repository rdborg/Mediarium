import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../../api'
import ServiceKeyCard from '../../components/ServiceKeyCard'
import { OPENSUBTITLES_COPY } from '../../components/serviceCopy'
import TestButton from '../../components/TestButton'
import { useToast } from '../../components/Toast'
import { SUBTITLE_LANGUAGES } from '../../languages'

function SubtitlesSection() {
  const [languages, setLanguages] = useState<string[]>(['en'])
  const [auto, setAuto] = useState(false)
  const [hasKey, setHasKey] = useState(false)
  const [status, setStatus] = useState('')
  const [sweeping, setSweeping] = useState(false)
  const toast = useToast()

  useEffect(() => {
    api.getSettings().then((s) => {
      setHasKey(!!s.hasOpenSubtitlesApiKey)
      setLanguages(s.subtitleLanguages?.length ? s.subtitleLanguages : ['en'])
      setAuto(s.subtitleAutoDownload ?? false)
    })
  }, [])

  function toggleLanguage(code: string) {
    setLanguages((cur) => (cur.includes(code) ? cur.filter((c) => c !== code) : [...cur, code]))
  }

  async function save() {
    try {
      await api.putSettings({ subtitleLanguages: languages, subtitleAutoDownload: auto })
      toast.success('Subtitle settings saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
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
      <h2>Languages and downloading</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        Mediarium keeps subtitles for the languages you pick next to every movie and episode, downloading the one timed for the
        same release when it can.
      </p>
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
      </fieldset>
      <label style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
        <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />
        Download automatically after import, and every few hours for anything missing
      </label>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button className="primary" disabled={languages.length === 0} onClick={save}>
          Save languages
        </button>
        <button disabled={!hasKey || sweeping} onClick={sweep}>
          Search for missing subtitles now
        </button>
      </div>
      {status && <span style={{ color: 'var(--text-dim)' }}>{status}</span>}
    </section>
  )
}

function OpenSubtitlesCard() {
  const [s, setS] = useState<SettingsData | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const toast = useToast()

  const load = useCallback(() => {
    api.getSettings().then((v) => {
      setS(v)
      setUsername(v.openSubtitlesAccountName ?? '')
    })
  }, [])
  useEffect(load, [load])

  async function saveAccount() {
    try {
      await api.putSettings({ openSubtitlesUsername: username.trim(), openSubtitlesPassword: password })
      setPassword('')
      toast.success(username.trim() ? 'OpenSubtitles account saved.' : 'OpenSubtitles account removed.')
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (!s) return <p>Loading…</p>
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
        <p style={{ color: 'var(--text-dim)', margin: 0 }}>
          Without an account, downloads are limited to about 5 per day (per internet connection). A free account raises that to about 20 per day, which
          matters when Mediarium fills in subtitles for a whole library. Your password is stored encrypted.
        </p>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" />
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
          <button className="primary" onClick={saveAccount} disabled={!username.trim() && !s.hasOpenSubtitlesAccount}>
            {username.trim() ? 'Save account' : 'Remove account'}
          </button>
          <TestButton
            label="Test login"
            disabled={!s.hasOpenSubtitlesApiKey}
            run={() => api.testService({ service: 'opensubtitles', username: username.trim(), password })}
          />
        </div>
      </div>
    </ServiceKeyCard>
  )
}

export default function SubtitleSettings() {
  return (
    <div className="settings-stack">
      <OpenSubtitlesCard />
      <SubtitlesSection />
    </div>
  )
}
