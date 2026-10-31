import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type IndexerConfig, type MediaServer, type QualityProfile, type Settings as SettingsData, type UsenetServer } from '../api'
import { useAuth } from '../AuthContext'
import { titleFor } from '../documentTitle'
import BrandMark from '../components/BrandMark'
import LogoGlyph from '../components/LogoGlyph'
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
import InfoLanguageCard from '../components/InfoLanguageCard'
import SetupMediaTypes from '../components/SetupMediaTypes'
import SetupPaths from '../components/SetupPaths'
import Switch from '../components/Switch'
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
import { chooseLanguage, currentLanguage, LANGUAGES, t, tc, tx, type Lang } from '../i18n'
import AmbientBackdrop from '../components/AmbientBackdrop'

// First-run onboarding wizard: linear, can't be skipped, each
// step validated before moving on. Every step is also independently
// reachable later from Settings — nothing here is a one-time-only choice.
const STEPS = [t('Admin account'), t('Media types'), t('Library paths'), t('Connect services'), t('Indexers & Search'), t('Usenet provider'), t('Quality & naming'), t('Media server'), t('VPN'), t('Done')] as const
const S = { admin: 0, types: 1, paths: 2, services: 3, sources: 4, usenet: 5, quality: 6, server: 7, vpn: 8, done: 9 } as const

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
  movies: t('Add the folder your movies go in, for example /movies.'),
  tv: t('Add the folder your TV shows go in, for example /tv.'),
  music: t('Add the folder your music goes in, for example /music.'),
  ebooks: t('Add the folder your ebooks go in, for example /books.'),
  audiobooks: t('Add the folder your audiobooks go in, for example /audiobooks.'),
  downloads: t('Add the folder downloads are saved in, for example /downloads.'),
}

const NAMING_CHOICES = [
  { id: 'plex', name: 'Plex', text: t('Title and year, for example "Night Harbour (2021)". Works with Plex and most other players.') },
  { id: 'jellyfin', name: 'Jellyfin / Emby', text: t('Title and year plus the quality in brackets, for example "[1080p]". For Jellyfin or Emby.') },
  { id: 'kodi', name: 'Kodi', text: t('Title and year plus quality and source, for example "[1080p Bluray]". For Kodi.') },
  { id: 'minimal', name: t('Simple'), text: t("Just the title, with episodes as S01E02. For when you don't use a media player.") },
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
    document.title = titleFor(t('Set up'))
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
    username: check.firstError(check.required(username, t('Choose a username to sign in with, at least 3 characters.')), check.username(username)),
    name: check.maxLength(name, 100, t('Your name')),
    email: check.email(email),
    password: check.firstError(check.required(password, t('Choose a password of at least 8 characters.')), check.password(password)),
    confirmPassword: check.firstError(check.required(confirmPassword, t('Type your password again to make sure it is right.')), check.passwordsMatch(password, confirmPassword)),
    legal: legalOk ? null : t('Read the notice and tick the box to continue.'),
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

  // The UDP switch is off until the person turns it on. Loaded when this step
  // opens, saved only when they press Continue.
  const [discoverOn, setDiscoverOn] = useState(false)
  const discoverLoaded = useRef(false)
  useEffect(() => {
    if (step !== S.vpn || discoverLoaded.current) return
    discoverLoaded.current = true
    api
      .getSettings()
      .then((s) => setDiscoverOn(!!s.discoverThroughTunnel))
      .catch(() => undefined)
  }, [step])

  // One step's work at a time: a second press while the first is still going
  // is ignored (it would otherwise fail with "already exists" after the first went through).
  const working = useRef(false)
  const legalSaved = useRef(true) // false when saving the legal flag after creating the account failed
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
      // The account exists now, so go on whatever happens next. If saving the
      // legal flag fails (a busy server, a dropped connection) it is tried
      // again with the next step; pressing Create account again would only
      // answer that an administrator already exists.
      try {
        await api.putSettings({ legalAcknowledged: true })
        legalSaved.current = true
      } catch {
        legalSaved.current = false
      }
      setStep(S.types)
    })
  }

  async function submitTypes() {
    if (nothingChosen(chosen)) {
      setLastOneHint(true)
      return
    }
    await guard(async () => {
      if (!legalSaved.current) {
        await api.putSettings({ legalAcknowledged: true })
        legalSaved.current = true
      }
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
      ← {t('Back')}
    </button>
  )

  async function saveDiscover() {
    await guard(async () => {
      await api.putSettings({ discoverThroughTunnel: discoverOn })
      setStep(S.done)
    })
  }

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
      <AmbientBackdrop level="full" />
      {step !== S.done && <h1 className="sr-only">{t('Set up Mediarium: {step}', { step: STEPS[step] })}</h1>}
      <div className="auth-logo wizard-logo">
        <LogoGlyph size={72} />
        <span>Mediarium</span>
      </div>
      <ol className="wizard-steps" aria-label={t('Set up Mediarium')}>
        {STEPS.map((label, i) => (
          <li key={label} className={i === step ? 'active' : i < step ? 'done' : ''} aria-current={i === step ? 'step' : undefined}>
            <b>{String(i + 1).padStart(2, '0')}</b>
            <span>{label}</span>
          </li>
        ))}
      </ol>

      {error && <p className="error-text">{error}</p>}

      {step === S.admin && (
        <div className="card grid-form">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
            <h2>{t('Create your account')}</h2>
            <select aria-label={t('App language')} value={currentLanguage()} onChange={(e) => chooseLanguage(e.target.value as Lang)} style={{ width: 'auto', fontSize: '0.85rem' }}>
              {LANGUAGES.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.name}
                </option>
              ))}
            </select>
          </div>
          <p className="wizard-lead">{t('This becomes the owner account.')}</p>
          <div className="form-cols admin-cols">
            {codeRequired && (
              <label className="setup-code">
                {t('Setup code')}
                <input value={setupCode} onChange={(e) => setSetupCode(e.target.value)} autoComplete="off" spellCheck={false} />
                <small className="field-hint">{t("You're not on your home network, so enter the one-time code from the log. With Docker, run: docker logs mediarium")}</small>
              </label>
            )}
            <label>
              {t('Username')}
              <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus {...adminCheck.bind('username', username, setUsername)} />
              <FieldError v={adminCheck} name="username" />
              <small className="field-hint">{t('You sign in with this. At least 3 characters.')}</small>
            </label>
            <label>
              {t('Name')}
              <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" {...adminCheck.bind('name', name, setName)} />
              <FieldError v={adminCheck} name="name" />
              <small className="field-hint">{t('Optional. Used in your greeting.')}</small>
            </label>
            <label>
              {t('Email')}
              <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" {...adminCheck.bind('email', email, setEmail)} />
              <FieldError v={adminCheck} name="email" />
              <small className="field-hint">{t('Optional. Used for notifications.')}</small>
            </label>
            <label>
              {t('Password')}
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" {...adminCheck.bind('password')} />
              <FieldError v={adminCheck} name="password" />
              <small className="field-hint">{t('At least 8 characters.')}</small>
            </label>
            <label>
              {t('Password again')}
              <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} autoComplete="new-password" {...adminCheck.bind('confirmPassword')} />
              <FieldError v={adminCheck} name="confirmPassword" />
              <small className="field-hint">{t('Type it once more to be sure.')}</small>
            </label>
            <div className="cell-check">
              <span className="cell-label">{t('Password strength')}</span>
              {password ? <PasswordStrength password={password} /> : <small className="field-hint">{t('Shows here as you type.')}</small>}
            </div>
          </div>
          <div className="legal-box">
            <strong>{t('Before you start')}</strong>
            <p>
              {t(
                "Mediarium is an automation and organising tool. It does not host, index or supply any content. It is meant for material you have the legal right to get: your own backups, public-domain and freely licensed works, and content you are licensed to use. You alone are responsible for what you download and for following your country's laws and your providers' terms. It comes for educational and personal use with no warranty.",
              )}
            </p>
            <label className="check-row">
              <input type="checkbox" checked={legalOk} onChange={(e) => setLegalOk(e.target.checked)} {...adminCheck.bind('legal')} />
              {t('I have read this and accept responsibility for how I use Mediarium.')}
            </label>
            <FieldError v={adminCheck} name="legal" />
          </div>
          <button className="primary" onClick={submitAdmin} disabled={busy}>
            {t('Continue')}
          </button>
          <FormProblem v={adminCheck} action="continue" />
        </div>
      )}

      {step === S.types && (
        <div className="card grid-form">
          <h2>{t('What do you want to manage?')}</h2>
          <p className="wizard-lead">{t('Pick what you want to manage. The next steps only ask about these.')}</p>
          <SetupMediaTypes chosen={chosen} onToggle={toggleType} lastOneHint={lastOneHint} />
          <div className="wizard-nav">
            <button className="primary" onClick={submitTypes} disabled={busy}>
              {t('Continue')}
            </button>
          </div>
        </div>
      )}

      {step === S.paths && (
        <div className="card grid-form">
          <h2>{t('Library paths')}</h2>
          <p className="wizard-lead">{t('The boxes show the folders your compose file maps, so most people can just press Continue. These are paths inside the container.')}</p>
          <SetupPaths
            chosen={chosen}
            values={folders}
            setValue={(k, v) => setFolders((cur) => ({ ...cur, [k]: v }))}
            mapped={svc ? foldersMapped(svc) : {}}
            inUse={svc ? foldersInUse(svc) : {}}
            v={pathCheck}
          />
          <p className="wizard-small">{t("Mediarium only adds files to these folders. It never deletes or overwrites what's already there unless you tell it to.")}</p>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={submitLibraryPath} disabled={busy}>
              {t('Continue')}
            </button>
          </div>
          <FormProblem v={pathCheck} action="continue" />
        </div>
      )}

      {step === S.services && (
        <div className="wizard-services">
          <div className="card">
            <h2>{t('Connect your services')}</h2>
            {video ? (
              <p className="wizard-lead">
                {t("A few free services provide movie details, lists and subtitles. They're built in, so there's nothing to set up. If a shared limit is ever hit, the dashboard tells you and you can add your own free key in Settings.")}
              </p>
            ) : (
              <p className="wizard-lead">{t('Music details come from MusicBrainz, which needs no account or key. Just press Continue.')}</p>
            )}
          </div>
          {video && !svc && <p>{t('Loading…')}</p>}
          {video && svc && (
            <>
              {svc.tmdbKeyBuiltIn ? (
                <ConnectedServiceCard
                  icon="film"
                  title="TMDB"
                  does={t('Movie and show details, posters, episode lists, Discover and the calendar.')}
                  limits={t("TMDB's limit is generous enough for home use.")}
                  own={t('Having trouble? Get a free key at themoviedb.org and add it under Settings > Info, lists and subtitles > Movie and show info.')}
                />
              ) : (
                <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={false} configured={svc.hasTmdbApiKey} onSaved={loadSvc} />
              )}
              <InfoLanguageCard />
              {svc.openSubtitlesKeyBuiltIn ? (
                <ConnectedServiceCard
                  icon="chat"
                  title="OpenSubtitles"
                  does={t('Finding and downloading subtitles.')}
                  limits={t('Searching is free. Downloads are capped at about 5 a day per connection, or about 20 with a free OpenSubtitles account.')}
                  own={t('Turn subtitles on under Settings > Info, lists and subtitles > Subtitles. Your own account or key goes there too.')}
                />
              ) : (
                <>
                  <ServiceKeyCard service="opensubtitles" {...OPENSUBTITLES_COPY} builtIn={false} configured={!!svc.hasOpenSubtitlesApiKey} onSaved={loadSvc} />
                  <p style={{ color: 'var(--text-dim)' }}>{t('Turn subtitles on under Settings > Info, lists and subtitles > Subtitles.')}</p>
                </>
              )}
              {svc.traktClientIdBuiltIn ? (
                <ConnectedServiceCard
                  icon="list"
                  title="Trakt"
                  does={t('Imports public Trakt lists so you can add everything on a list in one go.')}
                  limits={t('Everyone shares this key, and Trakt allows 500 requests every 5 minutes across all of them. List imports use very few.')}
                  own={t('If Trakt starts refusing requests, get a free Client ID and add it under Settings > Info, lists and subtitles > Import lists.')}
                />
              ) : (
                <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={false} configured={svc.hasTraktClientId} onSaved={loadSvc} />
              )}
            </>
          )}
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => setStep(S.sources)}>
              {t('Continue')}
            </button>
          </div>
        </div>
      )}

      {step === S.sources && (
        <div className="card grid-form">
          <h2>{t('Add your indexers')}</h2>
          <p className="wizard-lead">{t('Indexers are the sites Mediarium searches for downloads. Add at least one: choose Usenet or torrent, pick the indexer, paste its API key and click Add.')}</p>
          <div className="wizard-indexers">
            <IndexerForm onAdded={loadIndexers} />
            <div className="wizard-added">
              <strong>
                {t('Added so far')} {indexers.length > 0 && <small>({indexers.length})</small>}
              </strong>
              {indexers.length === 0 ? (
                <p className="wizard-lead" style={{ margin: '6px 0 0' }}>
                  {t('Nothing yet.')}
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
                        {t('Remove')}
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
              {t('Continue')}
            </button>
            {indexers.length === 0 && <button onClick={() => setStep(S.usenet)}>{t('Skip for now')}</button>}
          </div>
          {askNoIndexer && (
            <ConfirmDialog
              title={t('Continue without an indexer?')}
              confirmLabel={t('Continue anyway')}
              cancelLabel={t('Add one now')}
              onCancel={() => setAskNoIndexer(false)}
              onConfirm={() => {
                setAskNoIndexer(false)
                setStep(S.usenet)
              }}
            >
              <p>{t("Without an indexer, Mediarium can't find anything to download.")}</p>
              <p>{t('Add one any time under Settings > Indexers & Search. The dashboard will remind you.')}</p>
            </ConfirmDialog>
          )}
        </div>
      )}

      {step === S.usenet && (
        <div className="card grid-form">
          <h2>{t('Add your Usenet provider')}</h2>
          <p className="wizard-lead">{t('Mediarium downloads by itself, so all it needs is the news-server login from your Usenet provider. Skip this if you only use torrents.')}</p>
          <div className="wizard-indexers">
            <UsenetServerForm
              key={serverFormKey}
              initial={blankServer(servers.length === 0 ? 0 : 1)}
              submitLabel={t('Add provider')}
              onSubmit={submitUsenetServer}
              showPriority={false}
            />
            <div className="wizard-added">
              <strong>
                {t('Added so far')} {servers.length > 0 && <small>({servers.length})</small>}
              </strong>
              {servers.length === 0 ? (
                <p className="wizard-lead" style={{ margin: '6px 0 0' }}>
                  {t('Nothing yet.')}
                </p>
              ) : (
                <ul className="added-list">
                  {servers.map((x) => (
                    <li key={x.id}>
                      <span className="badge">{x.priority === 0 ? tc('server', 'Main') : tc('server', 'Backup')}</span>
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
                        {t('Remove')}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              {servers.length > 0 && <p className="wizard-small">{t('Add another provider as a backup if you have one. It fills in anything the main one is missing.')}</p>}
            </div>
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => (servers.length === 0 ? setAskNoServer(true) : setStep(S.quality))}>
              {t('Continue')}
            </button>
            {servers.length === 0 && <button onClick={() => setStep(S.quality)}>{t('Skip for now')}</button>}
          </div>
          {askNoServer && (
            <ConfirmDialog
              title={t('Continue without a Usenet provider?')}
              confirmLabel={t('Continue anyway')}
              cancelLabel={t('Add one now')}
              onCancel={() => setAskNoServer(false)}
              onConfirm={() => {
                setAskNoServer(false)
                setStep(S.quality)
              }}
            >
              <p>{t("Without your Usenet provider's login, Usenet downloads can't start. Torrents will still work.")}</p>
              <p>{t('Add it any time under Settings > Downloading > Usenet. The dashboard will remind you.')}</p>
            </ConfirmDialog>
          )}
        </div>
      )}

      {step === S.quality && (
        <div className="card grid-form">
          {video ? (
            <>
              <h2>{t('What quality do you want?')}</h2>
              <p className="wizard-lead">
                {tx('Pick the quality for {what} you add. {recommended} suits most people. Upgrades and your own profiles are under Settings > Library > Quality.', {
                  what: qualityFor,
                  recommended: <strong>1080p</strong>,
                })}
              </p>
              <div className="choice-grid" role="radiogroup" aria-label={t('Default quality')}>
                {profiles.map((p) => (
                  <button key={p.id} role="radio" aria-checked={profileId === p.id} className={`choice-card${profileId === p.id ? ' active' : ''}`} onClick={() => setProfileId(p.id)}>
                    <span className="choice-dot" aria-hidden="true" />
                    <strong>{p.name}</strong>
                    <small>{profileBlurb(p)}</small>
                  </button>
                ))}
                {profiles.length === 0 && <div className="skeleton" style={{ height: 90, gridColumn: '1 / -1' }} />}
              </div>
              {chosen.music && <p className="wizard-small">{t('Music quality is under Settings > Library > Quality.')}</p>}
              <hr className="soft-rule" />
              <h2>{t('How should your files be named?')}</h2>
              <p className="wizard-lead">{t('Every download is renamed and put in its own folder, so your media player shows the right poster, title and episode.')}</p>
              <div className="player-pick">
                <strong id="player-label">{t('Which media player do you use?')}</strong>
                <div className="chip-row" role="radiogroup" aria-labelledby="player-label">
                  {MEDIA_PLAYERS.map((p) => (
                    <button key={p.id} type="button" role="radio" aria-checked={player === p.id} className={`chip${player === p.id ? ' active' : ''}`} onClick={() => choosePlayer(p.id)}>
                      {p.label}
                    </button>
                  ))}
                </div>
                <small className="field-hint">{t('The naming style below follows your choice. It only affects new downloads.')}</small>
              </div>
              <div className="choice-grid" role="radiogroup" aria-label={t('Naming style')}>
                {NAMING_CHOICES.map((c) => (
                  <button key={c.id} role="radio" aria-checked={namingPreset === c.id} className={`choice-card${namingPreset === c.id ? ' active' : ''}`} onClick={() => setNamingPreset(c.id)}>
                    <span className="choice-dot" aria-hidden="true" />
                    <strong>{c.name}</strong>
                    <small>{c.text}</small>
                  </button>
                ))}
              </div>
              <div>
                <strong style={{ display: 'block', marginBottom: 6 }}>{t('What a movie will look like')}</strong>
                <NamingPreview preset={namingPreset} />
              </div>
            </>
          ) : (
            <>
              <h2>{t('Quality & naming')}</h2>
              <p className="wizard-lead">{t('Music quality and file names are under Settings > Library. Nothing to pick here, so press Continue.')}</p>
            </>
          )}
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={submitNaming} disabled={busy}>
              {t('Continue')}
            </button>
            {video && <button onClick={() => setStep(S.server)}>{t('Skip for now')}</button>}
          </div>
        </div>
      )}

      {step === S.server && (
        <div className="card grid-form">
          <h2>{t('Connect your media server')}</h2>
          <p className="wizard-lead">
            {t('Optional. Connect Plex, Jellyfin or Emby to get "Watch in" links and have new files show up there on their own. Or do it later under Settings > Connections > Media servers.')}
          </p>
          {(player === 'kodi' || player === 'other') && (
            <p className="wizard-small">
              {player === 'kodi'
                ? t("Kodi doesn't need connecting. Press Continue, or connect one of these anyway.")
                : t("Other players don't need connecting. Press Continue, or connect one of these anyway.")}
            </p>
          )}
          <SetupMediaServer preferred={preferredServer} servers={mediaServers} onAdded={loadMediaServers} />
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => setStep(S.vpn)}>
              {mediaServers.length > 0 ? t('Continue') : t('Skip for now')}
            </button>
          </div>
        </div>
      )}

      {step === S.vpn && (
        <div className="card grid-form">
          <h2>{t('UDP trackers and DHT through the VPN')}</h2>
          <p className="wizard-lead">
            {t('Optional. With the built-in VPN, UDP trackers and DHT stay off until you turn this on, which is why a torrent that only lists UDP trackers does not start. Names are looked up through the VPN too. Add the VPN itself later under Settings > Downloading > VPN and proxy, where you can change this.')}
          </p>
          <Switch
            checked={discoverOn}
            onChange={setDiscoverOn}
            label={t('UDP trackers and DHT through the VPN')}
            description={t("Announces to UDP trackers and DHT queries go out through the tunnel, so peers and trackers see the VPN address. Tracker and DHT names are looked up through the VPN's DNS, not from your home connection, and a failed lookup does not go out. While this is off, those are skipped and only HTTP trackers are used. A download that has already started keeps the previous choice until you retry it.")}
          />
          <div className="wizard-nav">
            {backButton}
            <button className="primary" onClick={() => void saveDiscover()} disabled={busy}>
              {t('Continue')}
            </button>
            <button onClick={() => setStep(S.done)}>{t('Skip for now')}</button>
          </div>
        </div>
      )}

      {step === S.done && (
        <div className="finish-card">
          <div className="finish-glow" aria-hidden="true" />
          <div className="finish-confetti" aria-hidden="true">
            {Array.from({ length: 28 }, (_, i) => (
              <i key={i} style={{ ['--i' as string]: i }} />
            ))}
          </div>
          <BrandMark className="finish-mark" />
          <h1>{tx('Welcome to {name}', { name: <span>Mediarium</span> })}</h1>
          {gaps === null ? (
            <p>{t('Checking your setup…')}</p>
          ) : requiredGaps(gaps).length === 0 ? (
            <p>{t('Everything is set up for {what}. Search for something to add, browse Discover, or import what you already have.', { what: mediaPhrase(chosen) })}</p>
          ) : (
            <p>{t("Your account is ready, but Mediarium can't find or download anything yet. Here's what's left, or do it later from the dashboard.")}</p>
          )}
          {gaps !== null && gaps.length > 0 && <SetupGaps gaps={gaps} onGo={(to) => void finish(to)} />}
          <div className="finish-points">
            <span>
              <Icon name="search" size={16} /> {t('Find it')}
            </span>
            <span>
              <Icon name="download" size={16} /> {t('Download it')}
            </span>
            <span>
              <Icon name="folder" size={16} /> {t('Filed and ready to watch')}
            </span>
          </div>
          <div className="wizard-nav">
            {backButton}
            <button className="primary big" onClick={() => void finish()} disabled={busy}>
              {t('Open Mediarium')}
            </button>
          </div>
        </div>
      )}
    </main>
  )
}
