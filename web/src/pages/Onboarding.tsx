import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type IndexerConfig, type MediaServer, type QualityProfile, type Settings as SettingsData, type UsenetServer } from '../api'
import { useAuth } from '../AuthContext'
import { titleFor } from '../documentTitle'
import BrandMark from '../components/BrandMark'
import Icon from '../components/Icon'
import ConfirmDialog from '../components/ConfirmDialog'
import ConnectedServiceCard from '../components/ConnectedServiceCard'
import IndexerForm from '../components/IndexerForm'
import NamingPreview from '../components/NamingPreview'
import { profileBlurb, sortProfiles } from '../components/qualityBlurb'
import PasswordStrength from '../components/PasswordStrength'
import SetupGaps from '../components/SetupGaps'
import ServiceKeyCard from '../components/ServiceKeyCard'
import { OPENSUBTITLES_COPY, TMDB_COPY, TRAKT_COPY } from '../components/serviceCopy'
import SetupMediaServer from '../components/SetupMediaServer'
import SetupMediaTypes from '../components/SetupMediaTypes'
import SetupPaths from '../components/SetupPaths'
import UsenetServerForm, { blankServer, type UsenetServerDraft } from '../components/UsenetServerForm'
import {
  coreParent,
  FOLDER_INFO,
  foldersToAsk,
  foldersToSave,
  hasVideo,
  MEDIA_PLAYERS,
  mediaPhrase,
  namingPresetFor,
  nothingChosen,
  pathInside,
  playerForServer,
  qualityPhrase,
  type Chosen,
  type FolderKind,
  type FolderValues,
  type MediaPlayer,
} from '../setupHelpers'
import { factsFromDashboard, requiredGaps, setupGaps, type SetupGap } from '../setupStatus'
import * as check from '../validate'
import { FieldError, FormProblem, useValidation, type Errors } from '../useValidation'

// First-run onboarding wizard: linear, can't be skipped, each
// step validated before moving on. Every step is also independently
// reachable later from Settings — nothing here is a one-time-only choice.
const STEPS = ['Admin account', 'Media types', 'Library paths', 'Connect services', 'Indexers & Search', 'Usenet provider', 'Quality & naming', 'Media server', 'Done'] as const
const S = { admin: 0, types: 1, paths: 2, services: 3, sources: 4, usenet: 5, quality: 6, server: 7, done: 8 } as const

// The setting each folder box is saved to.
const SETTING_FIELD = {
  movies: 'moviesPath',
  tv: 'tvPath',
  music: 'musicPath',
  ebooks: 'ebooksPath',
  audiobooks: 'audiobooksPath',
  downloads: 'downloadsPath',
} as const

const FOLDER_REQUIRED: Record<FolderKind, string> = {
  movies: 'Add the folder your movies go in, for example /movies.',
  tv: 'Add the folder your TV shows go in, for example /tv.',
  music: 'Add the folder your music goes in, for example /music.',
  ebooks: 'Add the folder your ebooks go in, for example /books.',
  audiobooks: 'Add the folder your audiobooks go in, for example /audiobooks.',
  downloads: 'Add the folder downloads are saved in, for example /downloads.',
}

const NAMING_CHOICES = [
  { id: 'plex', name: 'Plex', text: 'Title and year, for example "Night Harbour (2021)". Works with Plex and most other players.' },
  { id: 'jellyfin', name: 'Jellyfin / Emby', text: 'Title and year plus the quality in brackets, for example "[1080p]". For Jellyfin or Emby.' },
  { id: 'kodi', name: 'Kodi', text: 'Title and year plus quality and source, for example "[1080p Bluray]". For Kodi.' },
  { id: 'minimal', name: 'Simple', text: "Just the title, with episodes as S01E02. For when you don't use a media player." },
]

// What the settings say each folder is right now.
function foldersInUse(s: SettingsData): FolderValues {
  return {
    movies: s.moviesPath,
    tv: s.tvPath,
    music: s.musicPath,
    ebooks: s.ebooksPath,
    audiobooks: s.audiobooksPath,
    downloads: s.downloadsPath,
  }
}

// What the container maps (from MOVIES_DIR and friends). An older server does
// not send them, so the folder in use stands in.
function foldersMapped(s: SettingsData): FolderValues {
  const c = s.containerFolders
  const now = foldersInUse(s)
  const out: FolderValues = {}
  for (const k of Object.keys(FOLDER_INFO) as FolderKind[]) out[k] = c?.[k] || now[k] || FOLDER_INFO[k].fallback
  return out
}

export default function Onboarding({ startStep = 0 }: { startStep?: number }) {
  const { refresh } = useAuth()
  const navigate = useNavigate()
  const [step, setStep] = useState(startStep)
  useEffect(() => {
    document.title = titleFor('Set up')
  }, [])
  const [error, setError] = useState('')
  // Each step opens at the top, whatever the last one was scrolled to.
  const top = useRef<HTMLDivElement>(null)
  const firstStep = useRef(true)
  useEffect(() => {
    if (firstStep.current) {
      firstStep.current = false
      return
    }
    top.current?.scrollIntoView({ block: 'start' })
  }, [step])

  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [legalOk, setLegalOk] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  // Someone who is not on the home network needs the one-time code from the log.
  const [setupCode, setSetupCode] = useState('')
  const [codeRequired, setCodeRequired] = useState(false)
  useEffect(() => {
    api
      .onboardingStatus()
      .then((s) => setCodeRequired(!!s.setupCodeRequired))
      .catch(() => undefined)
  }, [])

  // Which kinds of media to manage (saved to Settings > Media types).
  const [chosen, setChosen] = useState<Chosen>({ movies: true, tv: true, music: false, ebooks: false, audiobooks: false })
  const [lastOneHint, setLastOneHint] = useState(false)
  const typesLoaded = useRef(false)
  useEffect(() => {
    if (step < S.types || typesLoaded.current) return
    typesLoaded.current = true
    api
      .modules()
      .then((m) => setChosen({ movies: m.movies?.enabled !== false, tv: m.tv?.enabled !== false, music: m.music?.enabled === true, ebooks: m.ebooks?.enabled === true, audiobooks: m.audiobooks?.enabled === true }))
      .catch(() => undefined)
  }, [step])
  function toggleType(key: keyof Chosen) {
    if (chosen[key] && Object.values({ ...chosen, [key]: false }).every((on) => !on)) {
      setLastOneHint(true)
      return
    }
    setLastOneHint(false)
    setChosen({ ...chosen, [key]: !chosen[key] })
  }

  const [svc, setSvc] = useState<SettingsData | null>(null)
  const [folders, setFolders] = useState<FolderValues>({})
  const foldersReady = useRef(false)
  const loadSvc = useCallback(() => {
    api
      .getSettings()
      .then((s) => {
        setSvc(s)
        // The boxes start from the folders the container maps, never from an
        // old saved value. Later reloads leave what the person typed alone.
        if (!foldersReady.current) {
          foldersReady.current = true
          const mapped = foldersMapped(s)
          setFolders(mapped)
          // Music, ebooks and audiobooks have no mapping of their own unless the
          // compose file adds one. When movies, TV and downloads share a parent
          // folder, put those inside it instead of an unmapped /music.
          const { parent, sample } = coreParent(mapped)
          if (parent) {
            for (const k of ['music', 'ebooks', 'audiobooks'] as const) {
              if (mapped[k] !== FOLDER_INFO[k].fallback) continue
              api
                .folderCheck(mapped[k] ?? '')
                .then((r) => {
                  if (r.exists && !(r.mountKnown && !r.mounted)) return
                  setFolders((cur) => (cur[k] === mapped[k] ? { ...cur, [k]: pathInside(parent, k, sample) } : cur))
                })
                .catch(() => undefined)
            }
          }
        }
      })
      .catch(() => undefined)
  }, [])

  const adminCheck = useValidation({
    username: check.firstError(check.required(username, 'Choose a username to sign in with, at least 3 characters.'), check.username(username)),
    name: check.maxLength(name, 100, 'Your name'),
    email: check.email(email),
    password: check.firstError(check.required(password, 'Choose a password of at least 8 characters.'), check.password(password)),
    confirmPassword: check.firstError(check.required(confirmPassword, 'Type your password again to make sure it is right.'), check.passwordsMatch(password, confirmPassword)),
    legal: legalOk ? null : 'Read the notice and tick the box to continue.',
  })

  const asked = foldersToAsk(chosen)
  const pathErrors: Errors = {}
  for (const k of asked) {
    pathErrors[k] = check.firstError(check.required(folders[k], FOLDER_REQUIRED[k]), check.folderPath(folders[k] ?? '', FOLDER_INFO[k].fallback))
  }
  for (const k of ['ebooks', 'audiobooks'] as const) {
    if (!asked.includes(k)) pathErrors[k] = check.folderPath(folders[k] ?? '', FOLDER_INFO[k].fallback)
  }
  const pathCheck = useValidation(pathErrors)

  // Quality and naming.
  const [player, setPlayer] = useState<MediaPlayer>('plex')
  const [namingPreset, setNamingPreset] = useState('plex')
  const playerPicked = useRef(false)
  function choosePlayer(p: MediaPlayer) {
    playerPicked.current = true
    setPlayer(p)
    setNamingPreset(namingPresetFor(p))
  }
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [profileId, setProfileId] = useState(0)
  useEffect(() => {
    if (step !== S.quality) return
    api
      .listProfiles()
      .then((r) => {
        setProfiles(sortProfiles(r.profiles))
        setProfileId((cur) => cur || r.defaultId)
      })
      .catch(() => undefined)
  }, [step])

  const [mediaServers, setMediaServers] = useState<MediaServer[]>([])
  const loadMediaServers = useCallback(() => {
    api.listMediaServers().then(setMediaServers).catch(() => undefined)
  }, [])
  // A media server the app already knows about tells us which player to preselect.
  useEffect(() => {
    if (step !== S.quality && step !== S.server) return
    api
      .listMediaServers()
      .then((list) => {
        setMediaServers(list)
        const known = playerForServer(list.map((x) => x.kind))
        if (known && !playerPicked.current) {
          playerPicked.current = true
          setPlayer(known)
          setNamingPreset(namingPresetFor(known))
        }
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
    if (step === S.usenet) loadServers()
  }, [step, loadServers])
  const [indexers, setIndexers] = useState<IndexerConfig[]>([])
  const [askNoIndexer, setAskNoIndexer] = useState(false)
  const loadIndexers = useCallback(() => {
    api.listIndexers().then(setIndexers).catch(() => undefined)
  }, [])
  useEffect(() => {
    if (step === S.sources) loadIndexers()
  }, [step, loadIndexers])

  useEffect(() => {
    if (step >= S.types) loadSvc()
  }, [step, loadSvc])

  // On the last step, look at what is really set up so the wizard does not say
  // "everything is set up" when there is no indexer or download provider.
  const [gaps, setGaps] = useState<SetupGap[] | null>(null)
  useEffect(() => {
    if (step !== S.done) return
    let cancelled = false
    api
      .dashboard()
      .then((d) => !cancelled && setGaps(setupGaps(factsFromDashboard(d, { movies: chosen.movies, tv: chosen.tv }))))
      .catch(() => !cancelled && setGaps([]))
    return () => {
      cancelled = true
    }
  }, [step, chosen.movies, chosen.tv])

  // One step's work at a time: a second press while the first is still going
  // is ignored (it would otherwise fail with "already exists" after the first went through).
  const working = useRef(false)
  const [busy, setBusy] = useState(false)
  async function guard(fn: () => Promise<void>) {
    if (working.current) return
    working.current = true
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      working.current = false
      setBusy(false)
    }
  }

  async function submitAdmin() {
    setError('')
    if (!adminCheck.attempt()) return
    await guard(async () => {
      await api.createAdmin({ username, password, name: name.trim() || undefined, email: email.trim() || undefined, setupCode: setupCode.trim() || undefined })
      await api.putSettings({ legalAcknowledged: true })
      setStep(S.types)
    })
  }

  async function submitTypes() {
    if (nothingChosen(chosen)) {
      setLastOneHint(true)
      return
    }
    await guard(async () => {
      await api.setModules({ movies: chosen.movies, tv: chosen.tv, music: chosen.music, ebooks: !!chosen.ebooks, audiobooks: !!chosen.audiobooks })
      loadSvc()
      setStep(S.paths)
    })
  }

  async function submitLibraryPath() {
    setError('')
    if (!pathCheck.attempt()) return
    await guard(async () => {
      const inUse = svc ? foldersInUse(svc) : {}
      const save = foldersToSave([...new Set<FolderKind>([...asked, 'ebooks', 'audiobooks'])], folders, inUse)
      const body: Partial<SettingsData> = {}
      for (const k of Object.keys(save) as FolderKind[]) body[SETTING_FIELD[k]] = save[k]
      if (Object.keys(body).length > 0) await api.putSettings(body)
      loadSvc()
      setStep(S.services)
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
      if (hasVideo(chosen)) await api.putSettings({ namingPreset, ...(profileId ? { defaultProfileId: profileId } : {}) })
      setStep(S.server)
    })
  }

  const back = () => {
    setError('')
    setStep((n) => Math.max(S.types, n - 1))
  }
  const backButton = (
    <button className="btn-back" onClick={back}>
      ← Back
    </button>
  )

  async function finish(then?: string) {
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
      if (then) navigate(then)
    })
  }

  const video = hasVideo(chosen)
  const qualityFor = qualityPhrase(chosen)
  const preferredServer = player === 'plex' || player === 'jellyfin' || player === 'emby' ? player : undefined

  return (
    <main className="wizard" ref={top}>
      {step !== S.done && <h1 className="sr-only">Set up Mediarium: {STEPS[step]}</h1>}
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

      {step === S.admin && (
        <div className="card grid-form">
          <h2>Create your account</h2>
          <p className="wizard-lead">This becomes the owner account.</p>
          <div className="form-cols admin-cols">
            <label>
              Username
              <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus {...adminCheck.bind('username', username, setUsername)} />
              <FieldError v={adminCheck} name="username" />
              <small className="field-hint">You sign in with this. At least 3 characters.</small>
            </label>
            <label>
              Name
              <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" {...adminCheck.bind('name', name, setName)} />
              <FieldError v={adminCheck} name="name" />
              <small className="field-hint">Optional. Used in your greeting.</small>
            </label>
            <label>
              Email
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" {...adminCheck.bind('email', email, setEmail)} />
              <FieldError v={adminCheck} name="email" />
              <small className="field-hint">Optional. Used for notifications.</small>
            </label>
            <label>
              Password
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" {...adminCheck.bind('password')} />
              <FieldError v={adminCheck} name="password" />
              <small className="field-hint">At least 8 characters.</small>
            </label>
            <label>
              Password again
              <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} autoComplete="new-password" {...adminCheck.bind('confirmPassword')} />
              <FieldError v={adminCheck} name="confirmPassword" />
              <small className="field-hint">Type it once more to be sure.</small>
            </label>
            <div className="cell-check">
              <span className="cell-label">Password strength</span>
              {password ? <PasswordStrength password={password} /> : <small className="field-hint">Shows here as you type.</small>}
            </div>
            {codeRequired && (
              <label>
                Setup code
                <input value={setupCode} onChange={(e) => setSetupCode(e.target.value)} autoComplete="off" spellCheck={false} />
                <small className="field-hint">
                  You're not on your home network, so enter the one-time code from the log. With Docker, run: docker logs mediarium
                </small>
              </label>
            )}
          </div>
          <div className="legal-box">
            <strong>Before you start</strong>
            <p>
              Mediarium is an automation and organising tool. It does not host, index or supply any content. It is meant for material you have the legal
              right to get: your own backups, public-domain and freely licensed works, and content you are licensed to use. You alone are responsible for
              what you download and for following your country&apos;s laws and your providers&apos; terms. It comes for educational and personal use
              with no warranty.
            </p>
            <label className="check-row">
              <input type="checkbox" checked={legalOk} onChange={(e) => setLegalOk(e.target.checked)} {...adminCheck.bind('legal')} />I have read this and accept responsibility for how I use Mediarium.
            </label>
            <FieldError v={adminCheck} name="legal" />
          </div>
          <button className="primary" onClick={submitAdmin} disabled={busy}>
            Continue
          </button>
          <FormProblem v={adminCheck} verb="continue" />
        </div>
      )}

      {step === S.types && (
        <div className="card grid-form">
          <h2>What do you want to manage?</h2>
          <p className="wizard-lead">
            Pick what you want to manage. The next steps only ask about these.
          </p>
          <SetupMediaTypes chosen={chosen} onToggle={toggleType} lastOneHint={lastOneHint} />
          <div className="wizard-nav">
            <button className="primary" onClick={submitTypes} disabled={busy}>
              Continue
            </button>
          </div>
        </div>
      )}

      {step === S.paths && (
        <div className="card grid-form">
          <h2>Library paths</h2>
          <p className="wizard-lead">
            The boxes show the folders your compose file maps, so most people can just press Continue. These are paths inside the container.
          </p>
          <SetupPaths
            chosen={chosen}
            values={folders}
            setValue={(k, v) => setFolders((cur) => ({ ...cur, [k]: v }))}
            mapped={svc ? foldersMapped(svc) : {}}
            inUse={svc ? foldersInUse(svc) : {}}
            v={pathCheck}
          />
          <p className="wizard-small">Mediarium only adds files to these folders. It never deletes or overwrites what&apos;s already there unless you tell it to.</p>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={submitLibraryPath} disabled={busy}>
              Continue
            </button>
          </div>
          <FormProblem v={pathCheck} verb="continue" />
        </div>
      )}

      {step === S.services && (
        <div className="wizard-services">
          <div className="card">
            <h2>Connect your services</h2>
            {video ? (
              <p className="wizard-lead">
                A few free services provide movie details, lists and subtitles. They&apos;re built in, so there&apos;s nothing to set up. If a shared limit is ever hit, the
                dashboard tells you and you can add your own free key in Settings.
              </p>
            ) : (
              <p className="wizard-lead">Music details come from MusicBrainz, which needs no account or key. Just press Continue.</p>
            )}
          </div>
          {video && !svc && <p>Loading…</p>}
          {video && svc && (
            <>
              {svc.tmdbKeyBuiltIn ? (
                <ConnectedServiceCard
                  icon="film"
                  title="TMDB"
                  does="Movie and show details, posters, episode lists, Discover and the calendar."
                  limits="TMDB's limit is generous enough for home use."
                  own="Having trouble? Get a free key at themoviedb.org and add it under Settings > Info, lists and subtitles > Movie info and lists."
                />
              ) : (
                <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={false} configured={svc.hasTmdbApiKey} onSaved={loadSvc} />
              )}
              {svc.openSubtitlesKeyBuiltIn ? (
                <ConnectedServiceCard
                  icon="chat"
                  title="OpenSubtitles"
                  does="Finding and downloading subtitles."
                  limits="Searching is free. Downloads are capped at about 5 a day per connection, or about 20 with a free OpenSubtitles account."
                  own="Turn subtitles on under Settings > Info, lists and subtitles > Subtitles. Your own account or key goes there too."
                />
              ) : (
                <>
                  <ServiceKeyCard service="opensubtitles" {...OPENSUBTITLES_COPY} builtIn={false} configured={!!svc.hasOpenSubtitlesApiKey} onSaved={loadSvc} />
                  <p style={{ color: 'var(--text-dim)' }}>Turn subtitles on under Settings &gt; Info, lists and subtitles &gt; Subtitles.</p>
                </>
              )}
              {svc.traktClientIdBuiltIn ? (
                <ConnectedServiceCard
                  icon="list"
                  title="Trakt"
                  does="Imports public Trakt lists so you can add everything on a list in one go."
                  limits="Everyone shares this key, and Trakt allows 500 requests every 5 minutes across all of them. List imports use very few."
                  own="If Trakt starts refusing requests, get a free Client ID and add it under Settings > Info, lists and subtitles > Movie info and lists."
                />
              ) : (
                <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={false} configured={svc.hasTraktClientId} onSaved={loadSvc} />
              )}
            </>
          )}
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => setStep(S.sources)}>
              Continue
            </button>
          </div>
        </div>
      )}

      {step === S.sources && (
        <div className="card grid-form">
          <h2>Add your indexers</h2>
          <p className="wizard-lead">
            Indexers are the sites Mediarium searches for downloads. Add at least one: choose Usenet or torrent, pick the indexer, paste its API key and click Add.
          </p>
          <div className="wizard-indexers">
            <IndexerForm onAdded={loadIndexers} />
            <div className="wizard-added">
              <strong>Added so far {indexers.length > 0 && <small>({indexers.length})</small>}</strong>
              {indexers.length === 0 ? (
                <p className="wizard-lead" style={{ margin: '6px 0 0' }}>
                  Nothing yet.
                </p>
              ) : (
                <ul className="added-list">
                  {indexers.map((i) => (
                    <li key={i.id}>
                      <span className={`badge proto-${i.protocol}`}>{i.protocol === 'torrent' ? 'Torrent' : 'Usenet'}</span>
                      <span className="added-name">{i.name}</span>
                      <button
                        className="btn-sm"
                        disabled={busy}
                        onClick={() =>
                          void guard(async () => {
                            await api.deleteIndexer(i.id)
                            loadIndexers()
                          })
                        }
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
            <button className="primary" onClick={() => (indexers.length === 0 ? setAskNoIndexer(true) : setStep(S.usenet))}>
              Continue
            </button>
            {indexers.length === 0 && <button onClick={() => setStep(S.usenet)}>Skip for now</button>}
          </div>
          {askNoIndexer && (
            <ConfirmDialog
              title="Continue without an indexer?"
              confirmLabel="Continue anyway"
              cancelLabel="Add one now"
              onCancel={() => setAskNoIndexer(false)}
              onConfirm={() => {
                setAskNoIndexer(false)
                setStep(S.usenet)
              }}
            >
              <p>
                Without an indexer, Mediarium can&apos;t find anything to download.
              </p>
              <p>Add one any time under Settings &gt; Indexers &amp; Search. The dashboard will remind you.</p>
            </ConfirmDialog>
          )}
        </div>
      )}

      {step === S.usenet && (
        <div className="card grid-form">
          <h2>Add your Usenet provider</h2>
          <p className="wizard-lead">
            Mediarium downloads by itself, so all it needs is the news-server login from your Usenet provider. Skip this if you only use torrents.
          </p>
          <div className="wizard-indexers">
            <UsenetServerForm
              key={serverFormKey}
              initial={blankServer(servers.length === 0 ? 0 : 1)}
              submitLabel="Add provider"
              onSubmit={submitUsenetServer}
              showPriority={false}
            />
            <div className="wizard-added">
              <strong>Added so far {servers.length > 0 && <small>({servers.length})</small>}</strong>
              {servers.length === 0 ? (
                <p className="wizard-lead" style={{ margin: '6px 0 0' }}>
                  Nothing yet.
                </p>
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
                        disabled={busy}
                        onClick={() =>
                          void guard(async () => {
                            await api.deleteUsenetServer(x.id)
                            loadServers()
                          })
                        }
                      >
                        Remove
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {servers.length > 0 && <p className="wizard-small">Add another provider as a backup if you have one. It fills in anything the main one is missing.</p>}
            </div>
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => (servers.length === 0 ? setAskNoServer(true) : setStep(S.quality))}>
              Continue
            </button>
            {servers.length === 0 && <button onClick={() => setStep(S.quality)}>Skip for now</button>}
          </div>
          {askNoServer && (
            <ConfirmDialog
              title="Continue without a Usenet provider?"
              confirmLabel="Continue anyway"
              cancelLabel="Add one now"
              onCancel={() => setAskNoServer(false)}
              onConfirm={() => {
                setAskNoServer(false)
                setStep(S.quality)
              }}
            >
              <p>Without your Usenet provider&apos;s login, Usenet downloads can&apos;t start. Torrents will still work.</p>
              <p>Add it any time under Settings &gt; Downloading &gt; Usenet and torrents. The dashboard will remind you.</p>
            </ConfirmDialog>
          )}
        </div>
      )}

      {step === S.quality && (
        <div className="card grid-form">
          {video ? (
            <>
              <h2>What quality do you want?</h2>
              <p className="wizard-lead">
                Pick the quality for {qualityFor} you add. <strong>1080p</strong> suits most people. Upgrades and your own profiles are under Settings &gt; Library &gt; Quality.
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
              {chosen.music && <p className="wizard-small">Music quality is under Settings &gt; Library &gt; Quality.</p>}
              <hr className="soft-rule" />
              <h2>How should your files be named?</h2>
              <p className="wizard-lead">
                Every download is renamed and put in its own folder, so your media player shows the right poster, title and episode.
              </p>
              <div className="player-pick">
                <strong id="player-label">Which media player do you use?</strong>
                <div className="chip-row" role="radiogroup" aria-labelledby="player-label">
                  {MEDIA_PLAYERS.map((p) => (
                    <button key={p.id} type="button" role="radio" aria-checked={player === p.id} className={`chip${player === p.id ? ' active' : ''}`} onClick={() => choosePlayer(p.id)}>
                      {p.label}
                    </button>
                  ))}
                </div>
                <small className="field-hint">The naming style below follows your choice. It only affects new downloads.</small>
              </div>
              <div className="choice-grid" role="radiogroup" aria-label="Naming style">
                {NAMING_CHOICES.map((c) => (
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
            </>
          ) : (
            <>
              <h2>Quality &amp; naming</h2>
              <p className="wizard-lead">
                Music quality and file names are under Settings &gt; Library. Nothing to pick here, so press Continue.
              </p>
            </>
          )}
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={submitNaming} disabled={busy}>
              Continue
            </button>
            {video && <button onClick={() => setStep(S.server)}>Skip for now</button>}
          </div>
        </div>
      )}

      {step === S.server && (
        <div className="card grid-form">
          <h2>Connect your media server</h2>
          <p className="wizard-lead">
            Optional. Connect Plex, Jellyfin or Emby to get &quot;Watch in&quot; links and have new files show up there on their own. Or do it later under
            Settings &gt; Connections &gt; Media servers.
          </p>
          {(player === 'kodi' || player === 'other') && (
            <p className="wizard-small">{player === 'kodi' ? "Kodi doesn't" : "Other players don't"} need connecting. Press Continue, or connect one of these anyway.</p>
          )}
          <SetupMediaServer preferred={preferredServer} servers={mediaServers} onAdded={loadMediaServers} />
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => setStep(S.done)}>
              {mediaServers.length > 0 ? 'Continue' : 'Skip for now'}
            </button>
          </div>
        </div>
      )}

      {step === S.done && (
        <div className="finish-card">
          <BrandMark className="finish-mark" />
          <h1>
            Welcome to <span>Mediarium</span>
          </h1>
          {gaps === null ? (
            <p>Checking your setup…</p>
          ) : requiredGaps(gaps).length === 0 ? (
            <p>
              Everything is set up for {mediaPhrase(chosen)}. Search for something to add, browse Discover, or import what you already have.
            </p>
          ) : (
            <p>Your account is ready, but Mediarium can&apos;t find or download anything yet. Here&apos;s what&apos;s left, or do it later from the dashboard.</p>
          )}
          {gaps !== null && gaps.length > 0 && <SetupGaps gaps={gaps} onGo={(to) => void finish(to)} />}
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
            <button className="primary big" onClick={() => void finish()} disabled={busy}>
              Open Mediarium
            </button>
          </div>
        </div>
      )}
    </main>
  )
}
