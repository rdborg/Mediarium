import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../api'
import { useAuth } from '../AuthContext'
import BrandMark from '../components/BrandMark'
import FolderStatus from '../components/FolderStatus'
import HardlinkWarning from '../components/HardlinkWarning'
import NamingPreview from '../components/NamingPreview'
import PasswordStrength from '../components/PasswordStrength'
import ServiceKeyCard from '../components/ServiceKeyCard'
import { OPENSUBTITLES_COPY, TMDB_COPY, TRAKT_COPY } from '../components/serviceCopy'
import UsenetServerForm, { blankServer, type UsenetServerDraft } from '../components/UsenetServerForm'

// First-run onboarding wizard (PRD §5.2): linear, can't be skipped, each
// step validated before moving on. Every step is also independently
// reachable later from Settings — nothing here is a one-time-only choice.
const STEPS = ['Admin account', 'Library paths', 'Connect services', 'Indexer', 'Usenet server', 'Naming', 'Done'] as const

export default function Onboarding() {
  const { refresh } = useAuth()
  const [step, setStep] = useState(0)
  const [error, setError] = useState('')

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [legalOk, setLegalOk] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  const [moviesPath, setMoviesPath] = useState('/movies')
  const [tvPath, setTvPath] = useState('/tv')
  const [svc, setSvc] = useState<SettingsData | null>(null)
  const loadSvc = useCallback(() => {
    api.getSettings().then(setSvc).catch(() => undefined)
  }, [])
  const [downloadsPath, setDownloadsPath] = useState('/downloads')

  const [indexerName, setIndexerName] = useState('')
  const [indexerBaseUrl, setIndexerBaseUrl] = useState('')
  const [indexerApiKey, setIndexerApiKey] = useState('')

  const [namingPreset, setNamingPreset] = useState('plex')

  useEffect(() => {
    if (step === 2) loadSvc()
  }, [step, loadSvc])

  async function guard(fn: () => Promise<void>) {
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function submitAdmin() {
    if (username.length < 3 || password.length < 8) {
      setError('Username must be 3+ characters, password 8+ characters.')
      return
    }
    if (!legalOk) {
      setError('Please read and accept the notice below to continue.')
      return
    }
    await guard(async () => {
      await api.createAdmin({ username, password, name: name.trim() || undefined, email: email.trim() || undefined })
      await api.putSettings({ legalAcknowledged: true })
      setStep(1)
    })
  }

  async function submitLibraryPath() {
    await guard(async () => {
      await api.putSettings({ moviesPath, tvPath, downloadsPath })
      setStep(2)
    })
  }

  async function submitIndexer(skip: boolean) {
    if (skip) {
      setStep(4)
      return
    }
    await guard(async () => {
      await api.createIndexer({ name: indexerName, definitionId: '', baseUrl: indexerBaseUrl, apiKey: indexerApiKey })
      setStep(4)
    })
  }

  async function submitUsenetServer(d: UsenetServerDraft) {
    await guard(async () => {
      await api.createUsenetServer({
        name: d.name,
        host: d.host.trim(),
        port: d.port,
        useSsl: d.useSsl,
        username: d.username,
        password: d.password,
        connections: d.connections,
        priority: 0,
        enabled: true,
      })
      setStep(5)
    })
  }

  async function submitNaming() {
    await guard(async () => {
      await api.putSettings({ namingPreset })
      setStep(6)
    })
  }

  async function finish() {
    await guard(async () => {
      await api.putSettings({ onboardingDone: true })
      await refresh()
    })
  }

  return (
    <div className="wizard">
      <div className="auth-logo wizard-logo">
        <BrandMark className="auth-mark" />
        <span>Mediarium</span>
      </div>
      <div className="wizard-steps">
        {STEPS.map((label, i) => (
          <span key={label} className={i === step ? 'active' : ''}>
            {i + 1}. {label}
          </span>
        ))}
      </div>

      {error && <p className="error-text">{error}</p>}

      {step === 0 && (
        <div className="card grid-form">
          <h2>Create your account</h2>
          <p style={{ color: 'var(--text-dim)', margin: 0 }}>This is the owner account. The dashboard greets you by name, or by username if you leave the name empty.</p>
          <div className="form-cols">
            <label>
              Username
              <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus />
            </label>
            <label>
              Name <small style={{ color: 'var(--text-dim)' }}>(optional, shown in your greeting)</small>
              <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
            </label>
            <label>
              Email <small style={{ color: 'var(--text-dim)' }}>(optional, used for notifications)</small>
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
            </label>
          </div>
          <label>
            Password
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </label>
          <PasswordStrength password={password} />
          <div className="legal-box">
            <strong>Before you start</strong>
            <p>
              Mediarium is an automation and organising tool. It does not host, index or supply any content, and it is meant for material you have the legal
              right to obtain: your own backups, public-domain and freely licensed works, and content you are licensed to use. You alone are responsible for
              what you download and for following the laws of your country and the terms of your providers. It is provided for educational and personal use
              with no warranty.
            </p>
            <label className="check-row">
              <input type="checkbox" checked={legalOk} onChange={(e) => setLegalOk(e.target.checked)} />I have read this and accept responsibility for how I use Mediarium.
            </label>
          </div>
          <button className="primary" onClick={submitAdmin}>
            Continue
          </button>
        </div>
      )}

      {step === 1 && (
        <div className="card grid-form">
          <h2>Library paths</h2>
          <label>
            Movies folder (inside the container)
            <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} />
          </label>
          <FolderStatus path={moviesPath} />
          <label>
            TV shows folder (inside the container)
            <input value={tvPath} onChange={(e) => setTvPath(e.target.value)} />
          </label>
          <FolderStatus path={tvPath} />
          <label>
            Downloads folder (inside the container)
            <input value={downloadsPath} onChange={(e) => setDownloadsPath(e.target.value)} />
          </label>
          <FolderStatus path={downloadsPath} />
          <HardlinkWarning pathA={moviesPath} pathB={downloadsPath} />
          <p style={{ color: 'var(--text-dim)', fontSize: '0.88rem' }}>
            Under Docker these are the paths <em>inside</em> the container, and each one must be mapped to a real folder on your machine in your
            compose file. Mediarium only adds files to them: it never deletes or overwrites anything that is already there unless you tell it to.
          </p>
          <button className="primary" onClick={submitLibraryPath}>
            Continue
          </button>
        </div>
      )}

      {step === 2 && (
        <div className="wizard-services">
          <div className="card">
            <h2>Connect your services</h2>
            <p style={{ color: 'var(--text-dim)' }}>
              Mediarium looks up movie and show details, subtitles and public lists from a few free services. Where this release already includes a key
              there is nothing to do. Anything you skip here shows up as a reminder on the dashboard, with what will not work until it is set up.
            </p>
          </div>
          {!svc ? (
            <p>Loading…</p>
          ) : (
            <>
              <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={!!svc.tmdbKeyBuiltIn} configured={svc.hasTmdbApiKey} onSaved={loadSvc} />
              <ServiceKeyCard
                service="opensubtitles"
                {...OPENSUBTITLES_COPY}
                builtIn={!!svc.openSubtitlesKeyBuiltIn}
                configured={!!svc.hasOpenSubtitlesApiKey}
                onSaved={loadSvc}
              />
              <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={!!svc.traktClientIdBuiltIn} configured={svc.hasTraktClientId} onSaved={loadSvc} />
            </>
          )}
          <div className="wizard-nav">
            <button className="primary" onClick={() => setStep(3)}>
              Continue
            </button>
            <button onClick={() => setStep(3)}>Skip for now</button>
          </div>
        </div>
      )}

      {step === 3 && (
        <div className="card grid-form">
          <h2>Add an indexer</h2>
          <p>At least one indexer is needed to search for releases. You can add more later in Settings.</p>
          <label>
            Name
            <input value={indexerName} onChange={(e) => setIndexerName(e.target.value)} placeholder="My Indexer" />
          </label>
          <label>
            Base URL
            <input value={indexerBaseUrl} onChange={(e) => setIndexerBaseUrl(e.target.value)} placeholder="https://api.example.com" />
          </label>
          <label>
            API key
            <input value={indexerApiKey} onChange={(e) => setIndexerApiKey(e.target.value)} />
          </label>
          <button className="primary" onClick={() => void submitIndexer(false)}>
            Continue
          </button>
          <button onClick={() => void submitIndexer(true)}>Skip for now</button>
        </div>
      )}

      {step === 4 && (
        <div className="card grid-form">
          <h2>Add your Usenet server</h2>
          <p style={{ color: 'var(--text-dim)' }}>
            Mediarium downloads for itself — there's no download program to connect. To download from Usenet it only needs
            the news-server account your Usenet provider gave you. Torrents need nothing, so skip this if you only use torrents;
            you can add or change servers later under Settings &gt; Downloads.
          </p>
          <UsenetServerForm
            initial={blankServer(0)}
            submitLabel="Continue"
            onSubmit={submitUsenetServer}
            showPriority={false}
            extra={<button onClick={() => setStep(5)}>Skip for now</button>}
          />
        </div>
      )}

      {step === 5 && (
        <div className="card grid-form">
          <h2>Naming preset</h2>
          <label>
            Preset
            <select value={namingPreset} onChange={(e) => setNamingPreset(e.target.value)}>
              <option value="plex">Plex</option>
              <option value="jellyfin">Jellyfin</option>
              <option value="kodi">Kodi</option>
              <option value="minimal">Minimal</option>
            </select>
          </label>
          <NamingPreview preset={namingPreset} />
          <button className="primary" onClick={submitNaming}>
            Continue
          </button>
        </div>
      )}

      {step === 6 && (
        <div className="card">
          <h2>You're all set</h2>
          <p>Search for your first movie once you're in.</p>
          <button className="primary" onClick={finish}>
            Finish
          </button>
        </div>
      )}
    </div>
  )
}
