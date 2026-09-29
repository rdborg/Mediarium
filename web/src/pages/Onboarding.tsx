import { useCallback, useEffect, useState } from 'react'
import { api, type IndexerConfig, type QualityProfile, type Settings as SettingsData, type UsenetServer } from '../api'
import { useAuth } from '../AuthContext'
import BrandMark from '../components/BrandMark'
import Icon from '../components/Icon'
import ConfirmDialog from '../components/ConfirmDialog'
import ConnectedServiceCard from '../components/ConnectedServiceCard'
import FolderStatus from '../components/FolderStatus'
import IndexerForm from '../components/IndexerForm'
import HardlinkWarning from '../components/HardlinkWarning'
import NamingPreview from '../components/NamingPreview'
import { profileBlurb, sortProfiles } from '../components/qualityBlurb'
import PasswordStrength from '../components/PasswordStrength'
import ServiceKeyCard from '../components/ServiceKeyCard'
import { OPENSUBTITLES_COPY, TMDB_COPY, TRAKT_COPY } from '../components/serviceCopy'
import UsenetServerForm, { blankServer, type UsenetServerDraft } from '../components/UsenetServerForm'

// First-run onboarding wizard: linear, can't be skipped, each
// step validated before moving on. Every step is also independently
// reachable later from Settings — nothing here is a one-time-only choice.
const STEPS = ['Admin account', 'Library paths', 'Connect services', 'Indexer', 'Usenet server', 'Quality & naming', 'Done'] as const

export default function Onboarding({ startStep = 0 }: { startStep?: number }) {
  const { refresh } = useAuth()
  const [step, setStep] = useState(startStep)
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


  const [namingPreset, setNamingPreset] = useState('plex')
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [profileId, setProfileId] = useState(0)
  useEffect(() => {
    if (step !== 5) return
    api
      .listProfiles()
      .then((r) => {
        setProfiles(sortProfiles(r.profiles))
        setProfileId((cur) => cur || r.defaultId)
      })
      .catch(() => undefined)
  }, [step])
  const [servers, setServers] = useState<UsenetServer[]>([])
  const [serverFormKey, setServerFormKey] = useState(0)
  const [askNoServer, setAskNoServer] = useState(false)
  const loadServers = useCallback(() => {
    api.listUsenetServers().then(setServers).catch(() => undefined)
  }, [])
  useEffect(() => {
    if (step === 4) loadServers()
  }, [step, loadServers])
  const [indexers, setIndexers] = useState<IndexerConfig[]>([])
  const [askNoIndexer, setAskNoIndexer] = useState(false)
  const loadIndexers = useCallback(() => {
    api.listIndexers().then(setIndexers).catch(() => undefined)
  }, [])
  useEffect(() => {
    if (step === 3) loadIndexers()
  }, [step, loadIndexers])

  useEffect(() => {
    if (step >= 1) loadSvc()
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

  // Adds the server and stays on the step, so more (for example a backup
  // provider) can be added; the first one is the main server, later ones are backups.
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
        priority: servers.length === 0 ? 0 : Math.max(...servers.map((x) => x.priority)) + 1,
        enabled: true,
      })
      setServerFormKey((k) => k + 1)
      loadServers()
    })
  }

  async function submitNaming() {
    await guard(async () => {
      await api.putSettings({ namingPreset, ...(profileId ? { defaultProfileId: profileId } : {}) })
      setStep(6)
    })
  }

  const back = () => {
    setError('')
    setStep((n) => Math.max(1, n - 1))
  }
  const backButton = (
    <button className="btn-back" onClick={back}>
      ← Back
    </button>
  )

  async function finish() {
    await guard(async () => {
      // Match "where to download from" to what was set up: only Usenet (a
      // news server or Usenet indexer, no torrent indexer) means Usenet only
      // with the torrent client off; only torrent indexers means torrents only.
      const [idx, srv] = await Promise.all([api.listIndexers().catch(() => []), api.listUsenetServers().catch(() => [])])
      const hasUsenet = srv.length > 0 || idx.some((i) => i.protocol === 'usenet')
      const hasTorrent = idx.some((i) => i.protocol === 'torrent')
      if (hasUsenet && !hasTorrent) await api.putSettings({ defaultSources: 'usenet', torrentEnabled: false })
      else if (hasTorrent && !hasUsenet) await api.putSettings({ defaultSources: 'torrent', torrentEnabled: true })
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
          <div className="path-cols">
            <div className="path-col">
              <label>
                Movies folder
                <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} />
              </label>
              <FolderStatus path={moviesPath} />
            </div>
            <div className="path-col">
              <label>
                TV shows folder
                <input value={tvPath} onChange={(e) => setTvPath(e.target.value)} />
              </label>
              <FolderStatus path={tvPath} />
            </div>
            <div className="path-col">
              <label>
                Downloads folder
                <input value={downloadsPath} onChange={(e) => setDownloadsPath(e.target.value)} />
              </label>
              <FolderStatus path={downloadsPath} />
            </div>
          </div>
          <HardlinkWarning pathA={moviesPath} pathB={downloadsPath} />
          <p style={{ color: 'var(--text-dim)', fontSize: '0.88rem' }}>
            These are the paths <em>inside</em> the container. Under Docker each one must be mapped to a real folder on your machine in your compose
            file. Mediarium only adds files to them: it never deletes or overwrites anything that is already there unless you tell it to.
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
              Mediarium uses a few free services for movie details, subtitles and lists. The ones below already come with Mediarium, so there is nothing to set up.
              They share limits between everyone, so if you ever hit one, Mediarium tells you on the dashboard and you can switch to your own free key in Settings.
            </p>
          </div>
          {!svc ? (
            <p>Loading…</p>
          ) : (
            <>
              {svc.tmdbKeyBuiltIn ? (
                <ConnectedServiceCard
                  icon="film"
                  title="TMDB"
                  does="Movie and show details, posters, episode lists, Discover and the calendar."
                  limits="TMDB allows plenty for home use, so you are unlikely to ever notice a limit."
                  own="If you ever have trouble, get a free key at themoviedb.org and enter it under Settings, Metadata & Lists."
                />
              ) : (
                <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={false} configured={svc.hasTmdbApiKey} onSaved={loadSvc} />
              )}
              {svc.openSubtitlesKeyBuiltIn ? (
                <ConnectedServiceCard
                  icon="chat"
                  title="OpenSubtitles"
                  does="Searching for and downloading subtitles. Subtitles that come inside a download are used first."
                  limits="Searching is free. Downloads are limited to about 5 a day per internet connection, or about 20 a day with a free personal OpenSubtitles account. Mediarium keeps under their limit of 5 requests a second."
                  own="Add your own account or key under Settings, Subtitles. Mediarium will warn you here before you run out."
                />
              ) : (
                <ServiceKeyCard service="opensubtitles" {...OPENSUBTITLES_COPY} builtIn={false} configured={!!svc.hasOpenSubtitlesApiKey} onSaved={loadSvc} />
              )}
              {svc.traktClientIdBuiltIn ? (
                <ConnectedServiceCard
                  icon="list"
                  title="Trakt"
                  does="Importing public Trakt lists into Discover so you can add everything on a list in one go."
                  limits="The key is shared by everyone using Mediarium: Trakt allows 500 requests every 5 minutes across all of them. List imports use very few, so this is rarely a problem."
                  own="If Trakt starts refusing requests, get a free Client ID and enter it under Settings, Metadata & Lists."
                />
              ) : (
                <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={false} configured={svc.hasTraktClientId} onSaved={loadSvc} />
              )}
            </>
          )}
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => setStep(3)}>
              Continue
            </button>
          </div>
        </div>
      )}

      {step === 3 && (
        <div className="card grid-form">
          <h2>Add your indexers</h2>
          <p style={{ color: 'var(--text-dim)', margin: 0 }}>
            An indexer is a search service for releases. Add at least one so Mediarium can find things to download, and as many as you like. Choose Usenet or torrent, pick the indexer, paste its API key, and press Add. You can add more later in Settings.
          </p>
          <div className="wizard-indexers">
            <IndexerForm onAdded={loadIndexers} />
            <div className="wizard-added">
              <strong>Added so far {indexers.length > 0 && <small>({indexers.length})</small>}</strong>
              {indexers.length === 0 ? (
                <p style={{ color: 'var(--text-dim)', margin: '6px 0 0' }}>Nothing yet.</p>
              ) : (
                <ul className="added-list">
                  {indexers.map((i) => (
                    <li key={i.id}>
                      <span className={`badge proto-${i.protocol}`}>{i.protocol === 'torrent' ? 'Torrent' : 'Usenet'}</span>
                      <span className="added-name">{i.name}</span>
                      <button
                        className="btn-sm"
                        onClick={async () => {
                          await api.deleteIndexer(i.id)
                          loadIndexers()
                        }}
                      >
                        Remove
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => (indexers.length === 0 ? setAskNoIndexer(true) : setStep(4))}>
              Continue
            </button>
            {indexers.length === 0 && <button onClick={() => setStep(4)}>Skip for now</button>}
          </div>
          {askNoIndexer && (
            <ConfirmDialog
              title="Continue without an indexer?"
              confirmLabel="Continue anyway"
              cancelLabel="Add one now"
              onCancel={() => setAskNoIndexer(false)}
              onConfirm={() => {
                setAskNoIndexer(false)
                setStep(4)
              }}
            >
              <p>
                You have not added an indexer yet. Mediarium needs at least one to find anything to download, so searching and automatic downloads will not work until you add one.
              </p>
              <p>You can add it any time under Settings, Indexers. The dashboard will remind you.</p>
            </ConfirmDialog>
          )}
        </div>
      )}

      {step === 4 && (
        <div className="card grid-form">
          <h2>Add your Usenet server</h2>
          <p style={{ color: 'var(--text-dim)' }}>
            Mediarium downloads for itself — there's no download program to connect. To download from Usenet it only needs
            the news-server account your Usenet provider gave you. Torrents need nothing, so skip this if you only use torrents;
            you can add or change servers later under Settings &gt; Downloads &amp; VPN.
          </p>
          <div className="wizard-indexers">
            <UsenetServerForm
              key={serverFormKey}
              initial={blankServer(servers.length === 0 ? 0 : 1)}
              submitLabel="Add server"
              onSubmit={submitUsenetServer}
              showPriority={false}
            />
            <div className="wizard-added">
              <strong>Added so far {servers.length > 0 && <small>({servers.length})</small>}</strong>
              {servers.length === 0 ? (
                <p style={{ color: 'var(--text-dim)', margin: '6px 0 0' }}>Nothing yet.</p>
              ) : (
                <ul className="added-list">
                  {servers.map((x) => (
                    <li key={x.id}>
                      <span className="badge">{x.priority === 0 ? 'Main' : 'Backup'}</span>
                      <span className="added-name" title={x.host}>
                        {x.name || x.host}
                      </span>
                      <button
                        className="btn-sm"
                        onClick={async () => {
                          await api.deleteUsenetServer(x.id)
                          loadServers()
                        }}
                      >
                        Remove
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {servers.length > 0 && <p style={{ color: 'var(--text-dim)', margin: '10px 0 0', fontSize: '0.85rem' }}>Add another provider as a backup if you have one; it fills in anything the main one is missing.</p>}
            </div>
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => (servers.length === 0 ? setAskNoServer(true) : setStep(5))}>
              Continue
            </button>
            {servers.length === 0 && <button onClick={() => setStep(5)}>Skip for now</button>}
          </div>
          {askNoServer && (
            <ConfirmDialog
              title="Continue without a Usenet server?"
              confirmLabel="Continue anyway"
              cancelLabel="Add one now"
              onCancel={() => setAskNoServer(false)}
              onConfirm={() => {
                setAskNoServer(false)
                setStep(5)
              }}
            >
              <p>Without your Usenet provider&apos;s server, NZB downloads cannot start. Torrents still work.</p>
              <p>You can add it any time under Settings, Downloads. The dashboard will remind you.</p>
            </ConfirmDialog>
          )}
        </div>
      )}

      {step === 5 && (
        <div className="card grid-form">
          <h2>What quality do you want?</h2>
          <p style={{ color: 'var(--text-dim)', margin: 0 }}>
            This is the default for everything you add. Mediarium looks for this quality and upgrades a download later if a better one appears, up to the limit.
            <strong> 1080p</strong> suits most people. You can pick a different one for any movie or show, and change or create your own under Settings, Quality.
          </p>
          <div className="choice-grid" role="radiogroup" aria-label="Default quality">
            {profiles.map((p) => (
              <button key={p.id} role="radio" aria-checked={profileId === p.id} className={`choice-card${profileId === p.id ? ' active' : ''}`} onClick={() => setProfileId(p.id)}>
                <span className="choice-dot" aria-hidden="true" />
                <strong>{p.name}</strong>
                <small>{profileBlurb(p)}</small>
              </button>
            ))}
            {profiles.length === 0 && <div className="skeleton" style={{ height: 90, gridColumn: '1 / -1' }} />}
          </div>
          <hr className="soft-rule" />
          <h2>How should your files be named?</h2>
          <p style={{ color: 'var(--text-dim)', margin: 0 }}>
            Mediarium renames every download and puts it in its own folder so your media player recognises it and shows the right poster, title and episode.
            Pick the player you use (or plan to use) to watch your movies and shows. If you are not sure, keep <strong>Plex</strong>: its layout also works with
            most other players. You can change this any time in Settings, Media Management, and it only affects new downloads.
          </p>
          <div className="choice-grid" role="radiogroup" aria-label="Naming style">
            {[
              { id: 'plex', name: 'Plex', text: 'Title and year, for example "Dune (2021)". For Plex, and a safe default for most players.' },
              { id: 'jellyfin', name: 'Jellyfin / Emby', text: 'Title and year plus the quality in brackets, for example "[1080p]". For Jellyfin or Emby.' },
              { id: 'kodi', name: 'Kodi', text: 'Title and year plus quality and source, for example "[1080p Bluray]". For Kodi.' },
              { id: 'minimal', name: 'Simple', text: 'The shortest names: just the title (episodes as S01E02). Only if you do not use a media player.' },
            ].map((c) => (
              <button key={c.id} role="radio" aria-checked={namingPreset === c.id} className={`choice-card${namingPreset === c.id ? ' active' : ''}`} onClick={() => setNamingPreset(c.id)}>
                <span className="choice-dot" aria-hidden="true" />
                <strong>{c.name}</strong>
                <small>{c.text}</small>
              </button>
            ))}
          </div>
          <div>
            <strong style={{ display: 'block', marginBottom: 6 }}>What a movie will look like</strong>
            <NamingPreview preset={namingPreset} />
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={submitNaming}>
              Continue
            </button>
            <button onClick={() => setStep(6)}>Skip for now</button>
          </div>
        </div>
      )}

      {step === 6 && (
        <div className="finish-card">
          <div className="finish-glow" aria-hidden="true" />
          <div className="finish-confetti" aria-hidden="true">
            {Array.from({ length: 28 }, (_, i) => (
              <i key={i} style={{ ['--i' as string]: i }} />
            ))}
          </div>
          <BrandMark className="finish-mark" />
          <h1>
            Welcome to <span>Mediarium</span>
          </h1>
          <p>Everything is set up. Search for a movie or show, browse Discover, or import what you already have. Mediarium takes it from there.</p>
          <div className="finish-points">
            <span>
              <Icon name="search" size={16} /> Find it
            </span>
            <span>
              <Icon name="download" size={16} /> Download it
            </span>
            <span>
              <Icon name="folder" size={16} /> Filed and ready to watch
            </span>
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary big" onClick={finish}>
              Open Mediarium
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

