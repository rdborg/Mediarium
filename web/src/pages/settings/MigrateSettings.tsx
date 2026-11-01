import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  api,
  type ArrApp,
  type MigrateBazarr,
  type MigratePreview,
  type MigrateRequest,
  type MigrateStatus,
  type MigrateSummary,
  type MigrateTitlesPreview,
  type PathMapping,
  type QualityProfile,
} from '../../api'
import Icon from '../../components/Icon'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import { DOCS_URL } from '../../docs'
import { importReason } from '../../importReasons'
import { locale, plural, t, tx, st } from '../../i18n'
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { apiKey as apiKeyCheck, firstError, folderPath, required, url as urlCheck } from '../../validate'
import TipLabel from '../../components/TipLabel'
import TypeTag from '../../components/TypeTag'

interface AppDef {
  key: ArrApp
  name: string
  what: string
  port: string
  help: string
  color: string
  auth?: 'userpass' // NZBGet signs in with a username and password, not a key
  hydra?: boolean // NZBHydra2 can also add its torrent feed
}

const GROUPS: { title: string; apps: AppDef[] }[] = [
  {
    title: t('Movies and TV shows'),
    apps: [
      { key: 'radarr', name: 'Radarr', what: t('your movies'), port: '7878', help: 'Settings → General → Security → API Key', color: '#ffc230' },
      { key: 'sonarr', name: 'Sonarr', what: t('your TV shows'), port: '8989', help: 'Settings → General → Security → API Key', color: '#35c5f4' },
      { key: 'medusa', name: 'Medusa', what: t('your TV shows'), port: '8081', help: 'Config → General → Interface → API key', color: '#5b8def' },
      { key: 'sickchill', name: 'SickChill', what: t('your TV shows'), port: '8081', help: 'Config → General → Interface → API key', color: '#8f6bd8' },
    ],
  },
  {
    title: t('Indexers'),
    apps: [
      { key: 'prowlarr', name: 'Prowlarr', what: t('your indexers'), port: '9696', help: 'Settings → General → Security → API Key', color: '#e66000' },
      { key: 'jackett', name: 'Jackett', what: t('your indexers'), port: '9117', help: t('The API Key at the top of the Jackett page'), color: '#c03a2b' },
      { key: 'nzbhydra', name: 'NZBHydra2', what: t('all your indexers as one'), port: '5076', help: 'Config → Main → Security → API key', color: '#2a9d8f', hydra: true },
    ],
  },
  {
    title: t('Usenet downloaders'),
    apps: [
      { key: 'sabnzbd', name: 'SABnzbd', what: t('your Usenet servers'), port: '8080', help: 'Config (cog) → General → Security → API Key', color: '#f5c518' },
      { key: 'nzbget', name: 'NZBGet', what: t('your Usenet servers'), port: '6789', help: t('The login you use for NZBGet (default nzbget / tegbzn6789)'), color: '#3fa34d', auth: 'userpass' },
    ],
  },
  {
    title: t('Requests and subtitles'),
    apps: [
      { key: 'overseerr', name: 'Overseerr / Jellyseerr', what: t('what people asked for'), port: '5055', help: 'Settings → General → API Key', color: '#7c6bf0' },
      { key: 'ombi', name: 'Ombi', what: t('what people asked for'), port: '3579', help: 'Settings → Configuration → API Key', color: '#e5813d' },
      { key: 'bazarr', name: 'Bazarr', what: t('your subtitle languages'), port: '6767', help: 'Settings → General → Security → API Key', color: '#c94f9e' },
    ],
  },
]
const ALL_APPS = GROUPS.flatMap((g) => g.apps)

interface Conn {
  url: string
  apiKey: string
  username: string
  password: string
  torznab: boolean
}
const emptyConn = (): Conn => ({ url: '', apiKey: '', username: '', password: '', torznab: false })

const ACTION_LABEL = { add: t('Will add'), exists: t('Already here'), skip: t('Skipped') } as const
const errText = (e: unknown) => (e instanceof Error ? e.message : String(e))

// Settings → System → Move from other apps: bring movies, shows,
// indexers, Usenet servers, requests and subtitle languages over from the
// apps you use today. Those apps are only read, never changed; files stay
// where they are.
export default function MigrateSettings() {
  const toast = useToast()
  const [conn, setConn] = useState<Record<ArrApp, Conn>>(() => Object.fromEntries(ALL_APPS.map((a) => [a.key, emptyConn()])) as Record<ArrApp, Conn>)
  const [preview, setPreview] = useState<MigratePreview | null>(null)
  const [pathMap, setPathMap] = useState<PathMapping[]>([])
  const [profileMapping, setProfileMapping] = useState<Record<string, number>>({})
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [include, setInclude] = useState({ movies: true, series: true, indexers: true, usenetServers: true, requests: true, subtitleLanguages: false })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState<MigrateStatus | null>(null)
  const [automationOn, setAutomationOn] = useState<boolean | null>(null)
  const timer = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  useEffect(() => {
    api.listProfiles().then((r) => setProfiles(r.profiles)).catch(() => undefined)
    api.getSettings().then((s) => setAutomationOn(s.automationEnabled !== false)).catch(() => undefined)
    api
      .migrateStatus()
      .then((st) => {
        if (st.running || st.finishedAt) setStatus(st)
        if (st.running) poll()
      })
      .catch(() => undefined)
    return () => clearInterval(timer.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const isConnected = (a: AppDef) => {
    const c = conn[a.key]
    return !!c.url.trim() && (a.auth === 'userpass' || !!c.apiKey.trim())
  }
  const connected = ALL_APPS.filter(isConnected)

  // An app counts as "in use" as soon as anything is typed for it. Then its address and key (or login) are needed.
  const inUse = ALL_APPS.filter((a) => {
    const c = conn[a.key]
    return !!(c.url.trim() || c.apiKey.trim() || c.username.trim() || c.password)
  })
  const connErrors: Record<string, string | null> = {}
  for (const a of inUse) {
    const c = conn[a.key]
    const example = `http://192.168.1.10:${a.port}`
    connErrors[`${a.key}.url`] = firstError(required(c.url, t('Add the address of {app}, for example {example}.', { app: a.name, example })), urlCheck(c.url, { example }))
    if (a.auth === 'userpass') connErrors[`${a.key}.username`] = required(c.username, t('Add the username you use to sign in to {app}.', { app: a.name }))
    else connErrors[`${a.key}.apiKey`] = firstError(required(c.apiKey, t('Add the API key from {app}. The hint under the box says where to find it.', { app: a.name })), apiKeyCheck(c.apiKey))
  }
  if (inUse.length === 0) connErrors.apps = t('Fill in the address and API key of at least one app first.')
  const v = useValidation(connErrors)

  function request(): MigrateRequest {
    const req: MigrateRequest = { pathMap: pathMap.filter((m) => m.from && m.to), profileMapping }
    for (const a of connected) {
      const c = conn[a.key]
      req[a.key] =
        a.auth === 'userpass'
          ? { url: c.url.trim(), username: c.username.trim(), password: c.password }
          : { url: c.url.trim(), apiKey: c.apiKey.trim(), ...(a.hydra && c.torznab ? { torznab: true } : {}) }
    }
    return req
  }

  async function runPreview() {
    if (!v.attempt()) return
    setBusy(true)
    setError('')
    try {
      const p = await api.migratePreview(request())
      setPreview(p)
      setPathMap(p.pathMap)
      const map: Record<string, number> = { ...profileMapping }
      for (const pr of [...(p.radarr?.profiles ?? []), ...(p.sonarr?.profiles ?? [])]) if (!(pr.arrName in map)) map[pr.arrName] = pr.profileId
      setProfileMapping(map)
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  function poll() {
    clearInterval(timer.current)
    // One slow or busy answer must not end the updates while the migration
    // carries on: give up only after a few misses in a row.
    let misses = 0
    timer.current = setInterval(async () => {
      try {
        const st = await api.migrateStatus()
        misses = 0
        setStatus(st)
        if (!st.running) clearInterval(timer.current)
      } catch (e) {
        if (++misses >= 5) {
          clearInterval(timer.current)
          setError(errText(e)) // say so, instead of showing a migration that seems to run for ever
        }
      }
    }, 1500)
  }

  async function run() {
    setBusy(true)
    setError('')
    try {
      const st = await api.migrateRun({ ...request(), include: { ...include, qualityProfiles: true } })
      setStatus(st)
      poll()
    } catch (e) {
      setError(errText(e))
    } finally {
      setBusy(false)
    }
  }

  async function setAutomation(on: boolean) {
    try {
      const s = await api.putSettings({ automationEnabled: on })
      setAutomationOn(s.automationEnabled !== false)
      toast.success(on ? t('Automatic searching is on again.') : t('Automatic searching paused.'))
    } catch (e) {
      toast.error(errText(e))
    }
  }

  const titleApps: { key: 'radarr' | 'sonarr' | 'medusa' | 'sickchill'; label: string; toggle: 'movies' | 'series' }[] = [
    { key: 'radarr', label: t('Movies from Radarr'), toggle: 'movies' },
    { key: 'sonarr', label: t('TV shows from Sonarr'), toggle: 'series' },
    { key: 'medusa', label: t('TV shows from Medusa'), toggle: 'series' },
    { key: 'sickchill', label: t('TV shows from SickChill'), toggle: 'series' },
  ]
  const rootFolders = titleApps.flatMap((ta) =>
    (preview?.[ta.key]?.rootFolders ?? []).map((r) => ({ ...r, from: ta.label, app: ALL_APPS.find((a) => a.key === ta.key)?.name ?? '' })),
  )
  // The folder boxes in step 2 (they appear after the preview). Blank is allowed and keeps the suggestion.
  const folderErrors: Record<string, string | null> = {}
  rootFolders.forEach((r, i) => {
    const idx = pathMap.findIndex((m) => m.from === r.path)
    folderErrors[`root${i}`] = folderPath(idx >= 0 ? pathMap[idx].to : r.mappedTo, '/media/movies')
  })
  const vf = useValidation(folderErrors)
  const profileRows = [...(preview?.radarr?.profiles ?? []), ...(preview?.sonarr?.profiles ?? [])]

  return (
    <div className="settings-stack">
      <p className="span-all" style={{ color: 'var(--text-dim)', margin: 0 }}>
        {t('Already using Radarr, Sonarr, Prowlarr, SABnzbd or similar? Bring your movies, shows, indexers, Usenet servers, requests and subtitle languages over. Those apps are only read, never changed, and your files stay where they are.')}{' '}
        <a href={`${DOCS_URL}/migrate.md`} target="_blank" rel="noreferrer">
          {t('Step-by-step guide')}
        </a>
      </p>

      {/* 1. Connect */}
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="key" size={14} /> <TipLabel help="adding#moving-from-radarr-sonarr-and-other-apps">{t('1. Connect your current apps')}</TipLabel>
        </legend>
        {GROUPS.map((g) => (
          <div key={g.title} className="mig-group">
            <h2>{g.title}</h2>
            <div className="mig-apps">
              {g.apps.map((a) => {
                const c = conn[a.key]
                const set = (patch: Partial<Conn>) => setConn({ ...conn, [a.key]: { ...c, ...patch } })
                return (
                  <div key={a.key} className="mig-app" style={{ ['--ac' as string]: a.color }}>
                    <div className="mig-app-head">
                      <span className="mig-dot" /> <strong>{a.name}</strong> <small>{a.what}</small>
                    </div>
                    <label>
                      {t('Address')}
                      <input value={c.url} onChange={(e) => set({ url: e.target.value })} placeholder={`http://192.168.1.10:${a.port}`} {...v.bind(`${a.key}.url`, c.url, (t) => set({ url: t }))} />
                      <FieldError v={v} name={`${a.key}.url`} />
                    </label>
                    {a.auth === 'userpass' ? (
                      <div className="mig-two">
                        <label>
                          {t('Username')}
                          <input value={c.username} onChange={(e) => set({ username: e.target.value })} autoComplete="off" {...v.bind(`${a.key}.username`, c.username, (t) => set({ username: t }))} />
                          <FieldError v={v} name={`${a.key}.username`} />
                        </label>
                        <label>
                          {t('Password')}
                          <input type="password" value={c.password} onChange={(e) => set({ password: e.target.value })} autoComplete="new-password" />
                        </label>
                      </div>
                    ) : (
                      <label>
                        {t('API key')}
                        <input value={c.apiKey} onChange={(e) => set({ apiKey: e.target.value })} autoComplete="off" {...v.bind(`${a.key}.apiKey`, c.apiKey, (t) => set({ apiKey: t }))} />
                        <FieldError v={v} name={`${a.key}.apiKey`} />
                      </label>
                    )}
                    <small>{a.help}</small>
                    {a.hydra && (
                      <label className="mig-check">
                        <input type="checkbox" checked={c.torznab} onChange={(e) => set({ torznab: e.target.checked })} /> {t('It also has torrent indexers (add its torrent feed too)')}
                      </label>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        ))}
        <div className="form-actions" style={{ marginTop: 14 }}>
          <button className="primary btn-with-icon" onClick={() => void runPreview()} disabled={busy}>
            <Icon name="search" size={15} /> {busy && !status?.running ? t('Reading your apps…') : t('Check and preview')}
          </button>
          <small style={{ color: 'var(--text-dim)', alignSelf: 'center' }}>{t('Fill in the ones you use and leave the rest empty. Nothing is imported yet.')}</small>
        </div>
        <FieldError v={v} name="apps" />
        {inUse.length > 0 && <FormProblem v={v} action="checkThem" />}
        {error && <p className="error-text">{error}</p>}
      </fieldset>

      {preview && (
        <>
          {/* 2. Folders */}
          {rootFolders.length > 0 && (
            <fieldset className="group usenet span-all">
              <legend>
                <Icon name="folder" size={14} /> {t('2. Check your folders')}
              </legend>
              <p style={{ marginTop: 0, color: 'var(--text-dim)' }}>
                {t('Your other apps may see your folders under different paths than Mediarium ({movies} for movies, {tv} for TV). Point each of their folders at the same place here. Green means found.', {
                  movies: preview.moviesPath,
                  tv: preview.tvPath,
                })}
              </p>
              <table className="mig-roots">
                <thead>
                  <tr>
                    <th>{t('App')}</th>
                    <th>{t('Their folder')}</th>
                    <th>{t('Same folder in Mediarium')}</th>
                    <th>{t('Found')}</th>
                  </tr>
                </thead>
                <tbody>
                  {rootFolders.map((r, i) => {
                    const idx = pathMap.findIndex((m) => m.from === r.path)
                    return (
                      <tr key={`${r.from}-${r.path}-${i}`}>
                        <td>
                          <small>{r.app}</small>
                        </td>
                        <td>
                          <code>{r.path}</code>
                        </td>
                        <td>
                          <input
                            value={idx >= 0 ? pathMap[idx].to : r.mappedTo}
                            onChange={(e) => {
                              const next = pathMap.filter((m) => m.from !== r.path)
                              next.push({ from: r.path, to: e.target.value })
                              setPathMap(next)
                            }}
                            {...vf.bind(`root${i}`, idx >= 0 ? pathMap[idx].to : r.mappedTo, (t) => setPathMap([...pathMap.filter((m) => m.from !== r.path), { from: r.path, to: t }]))}
                          />
                          <FieldError v={vf} name={`root${i}`} />
                          {r.suggested && <small className="mig-hint">{t('Suggested')}</small>}
                        </td>
                        <td>
                          <span className={`badge ${r.exists && r.foldersFound > 0 ? 'downloaded' : 'failed'}`}>
                            {plural(r.titles, '{found} of {n} title folder', '{found} of {n} title folders', { found: r.foldersFound })}
                          </span>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              <button
                className="btn-sm"
                style={{ marginTop: 10 }}
                onClick={() => {
                  if (vf.attempt()) void runPreview()
                }}
                disabled={busy}
              >
                {t('Check again with these folders')}
              </button>
              <FormProblem v={vf} action="checkAgain" />
            </fieldset>
          )}

          {/* 3. What will happen */}
          <fieldset className="group torrent span-all">
            <legend>
              <Icon name="list" size={14} /> {t('3. What will be imported')}
            </legend>
            <div className="mig-summary">
              {titleApps.map((t) => {
                const p = preview[t.key]
                return p ? <TitlesCard key={t.key} label={t.label} kind={t.toggle === 'movies' ? 'movie' : 'tv'} p={p} on={include[t.toggle]} onToggle={(v) => setInclude({ ...include, [t.toggle]: v })} /> : null
              })}
              {(['prowlarr', 'jackett', 'nzbhydra'] as const).map((k) => {
                const p = preview[k]
                if (!p) return null
                const name = ALL_APPS.find((a) => a.key === k)!.name
                return (
                  <SimpleCard key={k} label={t('Indexers from {app}', { app: name })} ok={p.ok} error={st(p.error)} summary={p.summary} on={include.indexers} onToggle={(v) => setInclude({ ...include, indexers: v })}>
                    {p.items.map((i) => (
                      <li key={`${i.name}-${i.baseUrl ?? ''}`}>
                        <span className={`mig-act act-${i.action}`}>{ACTION_LABEL[i.action]}</span> {i.name} <small>{i.protocol}</small>
                        {i.reason && <small> · {importReason(i.reason)}</small>}
                      </li>
                    ))}
                  </SimpleCard>
                )
              })}
              {(['sabnzbd', 'nzbget'] as const).map((k) => {
                const p = preview[k]
                if (!p) return null
                const name = ALL_APPS.find((a) => a.key === k)!.name
                return (
                  <SimpleCard key={k} label={t('Usenet servers from {app}', { app: name })} ok={p.ok} error={st(p.error)} summary={p.summary} on={include.usenetServers} onToggle={(v) => setInclude({ ...include, usenetServers: v })}>
                    {p.items.map((s) => (
                      <li key={`${s.host}-${s.username}`}>
                        <span className={`mig-act act-${s.action}`}>{ACTION_LABEL[s.action]}</span> {s.name || s.host} <small>{s.priority === 0 ? t('main') : t('backup {n}', { n: s.priority })}</small>
                        {s.reason && <small> · {importReason(s.reason)}</small>}
                      </li>
                    ))}
                  </SimpleCard>
                )
              })}
              {(['overseerr', 'ombi'] as const).map((k) => {
                const p = preview[k]
                if (!p) return null
                const name = k === 'overseerr' ? 'Overseerr / Jellyseerr' : 'Ombi'
                return (
                  <SimpleCard key={k} label={t('Requests from {app}', { app: name })} ok={p.ok} error={st(p.error)} summary={p.summary} on={include.requests} onToggle={(v) => setInclude({ ...include, requests: v })}>
                    {p.items.map((r) => (
                      <li key={`${r.mediaType}-${r.tmdbId ?? r.tvdbId}-${r.title}`}>
                        <span className={`mig-act act-${r.action}`}>{ACTION_LABEL[r.action]}</span> {r.title} {r.year ? <small>({r.year})</small> : null}
                        <small>
                          {' '}
                          · {r.mediaType === 'movie' ? t('movie') : t('show')} · {r.status}
                          {r.requestedBy.length ? ' · ' + t('asked by {names}', { names: r.requestedBy.join(', ') }) : ''}
                          {r.reason ? ` · ${importReason(r.reason)}` : ''}
                        </small>
                      </li>
                    ))}
                  </SimpleCard>
                )
              })}
              {preview.bazarr && <BazarrCard p={preview.bazarr} on={include.subtitleLanguages} onToggle={(v) => setInclude({ ...include, subtitleLanguages: v })} />}
            </div>

            {profileRows.length > 0 && (
              <div className="mig-profiles">
                <strong>{t('Quality profiles')}</strong>
                <table>
                  <tbody>
                    {profileRows.map((pr) => (
                      <tr key={`${pr.app}-${pr.arrId}`}>
                        <td>
                          {pr.app === 'radarr' ? 'Radarr' : 'Sonarr'}: <strong>{pr.arrName}</strong> <small>{plural(pr.titles, '({n} title)', '({n} titles)')}</small>
                        </td>
                        <td>→</td>
                        <td>
                          <select value={profileMapping[pr.arrName] ?? pr.profileId} onChange={(e) => setProfileMapping({ ...profileMapping, [pr.arrName]: Number(e.target.value) })}>
                            <option value={0}>{t('Default profile')}</option>
                            {profiles.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.name}
                              </option>
                            ))}
                          </select>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </fieldset>

          {/* 4. Import */}
          <fieldset className="group alerts span-all">
            <legend>
              <Icon name="download" size={14} /> {t('4. Import')}
            </legend>
            <p style={{ marginTop: 0 }}>
              {t("Files stay where they are, and nothing is moved, copied or downloaded. Pause automatic searching until you switch over, so your old apps and Mediarium don't grab the same title.")}
            </p>
            {automationOn !== null && (
              <Switch
                checked={!automationOn}
                onChange={(v) => void setAutomation(!v)}
                label={t('Pause automatic searching while I test')}
                tip={t('Your old apps and Mediarium would both search for the same missing titles. Pausing stops that until you switch over.')}
                description={t('Nothing searches or downloads on its own until you turn this off.')}
                showState
              />
            )}
            <div className="form-actions" style={{ marginTop: 14 }}>
              <button className="primary btn-with-icon big" onClick={() => void run()} disabled={busy || !!status?.running}>
                <Icon name="download" size={16} /> {status?.running ? t('Importing…') : t('Import now')}
              </button>
            </div>
          </fieldset>
        </>
      )}

      {status && (status.running || status.finishedAt) && <Progress status={status} />}
    </div>
  )
}

function Counts({ s }: { s: MigrateSummary }) {
  return (
    <div className="mig-counts">
      <span className="act-add">{t('{n} to add', { n: s.add })}</span>
      <span className="act-exists">{t('{n} already here', { n: s.exists })}</span>
      <span className="act-skip">{t('{n} skipped', { n: s.skip })}</span>
    </div>
  )
}

function TitlesCard({ label, kind, p, on, onToggle }: { label: string; kind: 'movie' | 'tv'; p: MigrateTitlesPreview; on: boolean; onToggle: (v: boolean) => void }) {
  const [open, setOpen] = useState(false)
  if (!p.ok)
    return (
      <div className="mig-card bad">
        <strong>{label}</strong>
        <p className="error-text">{st(p.error)}</p>
      </div>
    )
  const skipped = p.items.filter((i) => i.action === 'skip')
  return (
    <div className={`mig-card${on ? '' : ' off'}`}>
      <label className="mig-card-head">
        <input type="checkbox" checked={on} onChange={(e) => onToggle(e.target.checked)} /> <strong>{label}</strong>
        {p.version && <small>v{p.version}</small>}
      </label>
      <Counts s={p.summary} />
      {skipped.length > 0 && <small className="mig-hint">{t("Skipped usually means the folder wasn't found. Check step 2.")}</small>}
      <button className="btn-sm" onClick={() => setOpen((v) => !v)}>
        {open ? t('Hide the list') : t('Show all {n}', { n: p.items.length })}
      </button>
      {open && (
        <ul className="mig-list">
          {p.items.map((i) => (
            <li key={`${i.tmdbId ?? i.tvdbId}-${i.title}`}>
              <span className={`mig-act act-${i.action}`}>{ACTION_LABEL[i.action]}</span> <TypeTag kind={kind} />
              {i.title} {i.year ? <small>({i.year})</small> : null}
              <small>
                {' '}
                · {plural(i.files, '{n} file', '{n} files')}
                {i.quality ? ` · ${i.quality}` : ''}
                {i.reason ? ` · ${importReason(i.reason)}` : ''}
              </small>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function SimpleCard({ label, ok, error, summary, on, onToggle, children }: { label: string; ok: boolean; error?: string; summary: MigrateSummary; on: boolean; onToggle: (v: boolean) => void; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  if (!ok)
    return (
      <div className="mig-card bad">
        <strong>{label}</strong>
        <p className="error-text">{error}</p>
      </div>
    )
  return (
    <div className={`mig-card${on ? '' : ' off'}`}>
      <label className="mig-card-head">
        <input type="checkbox" checked={on} onChange={(e) => onToggle(e.target.checked)} /> <strong>{label}</strong>
      </label>
      <Counts s={summary} />
      <button className="btn-sm" onClick={() => setOpen((v) => !v)}>
        {open ? t('Hide the list') : t('Show the list')}
      </button>
      {open && <ul className="mig-list">{children}</ul>}
    </div>
  )
}

// Bazarr: only its language choice can move over, and it replaces the
// subtitle languages in Mediarium, so it starts switched off.
function BazarrCard({ p, on, onToggle }: { p: MigrateBazarr; on: boolean; onToggle: (v: boolean) => void }) {
  const [open, setOpen] = useState(false)
  if (!p.ok)
    return (
      <div className="mig-card bad">
        <strong>{t('Subtitle languages from Bazarr')}</strong>
        <p className="error-text">{st(p.error)}</p>
      </div>
    )
  const noEquivalent = (p.providers ?? []).filter((x) => !x.equivalent)
  return (
    <div className={`mig-card${on ? '' : ' off'}`}>
      <label className="mig-card-head">
        <input type="checkbox" checked={on} onChange={(e) => onToggle(e.target.checked)} /> <strong>{t('Subtitle languages from Bazarr')}</strong>
        {p.version && <small>v{p.version}</small>}
      </label>
      <div className="mig-counts">
        {p.languages.map((l) => (
          <span key={l.code} className={l.action === 'skip' ? 'act-skip' : 'act-add'} title={l.reason}>
            {l.name}
          </span>
        ))}
      </div>
      <small className="mig-hint">{t('Replaces the subtitle languages in Settings → Info, lists and subtitles → Subtitles.')}</small>
      {noEquivalent.length > 0 && (
        <>
          <button className="btn-sm" onClick={() => setOpen((v) => !v)}>
            {open ? t('Hide') : plural(noEquivalent.length, '{n} Bazarr subtitle source has no equivalent here', '{n} Bazarr subtitle sources have no equivalent here')}
          </button>
          {open && <ul className="mig-list">{noEquivalent.map((x) => <li key={x.name}>{x.name}</li>)}</ul>}
        </>
      )}
    </div>
  )
}

function Progress({ status }: { status: MigrateStatus }) {
  const pct = status.total > 0 ? Math.round((status.done / status.total) * 100) : status.running ? 5 : 100
  const r = status.results ?? {}
  const rows: [string, typeof r.movies][] = [
    [t('Movies'), r.movies],
    [t('TV shows'), r.series],
    [t('Indexers'), r.indexers],
    [t('Usenet servers'), r.usenetServers],
    [t('Requests'), r.requests],
    [t('Subtitle languages'), r.subtitleLanguages],
  ]
  return (
    <fieldset className="group folders span-all">
      <legend>
        <Icon name="activity" size={14} /> {status.running ? t('Importing…') : status.step === 'failed' ? t('Import stopped') : t('Import finished')}
      </legend>
      <div className="mig-progress">
        <div style={{ width: `${pct}%` }} />
      </div>
      <small style={{ color: 'var(--text-dim)' }}>
        {status.running
          ? t('{step}: {done} of {total}', { step: status.step, done: status.done, total: status.total })
          : status.finishedAt
            ? t('Finished {date}', { date: new Date(status.finishedAt).toLocaleString(locale()) })
            : ''}
      </small>
      <div className="mig-summary" style={{ marginTop: 12 }}>
        {rows
          .filter(([, c]) => c)
          .map(([label, c]) => (
            <div key={label} className="mig-card">
              <strong>{label}</strong>
              <div className="mig-counts">
                <span className="act-add">{t('{n} added', { n: c!.added })}</span>
                <span className="act-exists">{t('{n} already here', { n: c!.existing })}</span>
                {c!.skipped > 0 && <span className="act-skip">{t('{n} skipped', { n: c!.skipped })}</span>}
                {c!.failed > 0 && <span className="act-fail">{t('{n} failed', { n: c!.failed })}</span>}
              </div>
              {c!.filesLinked > 0 && <small>{plural(c!.filesLinked, '{n} existing file linked', '{n} existing files linked')}</small>}
              {c!.disabled > 0 && <small> · {t('{n} added but switched off, so check them', { n: c!.disabled })}</small>}
            </div>
          ))}
      </div>
      {(status.notes ?? []).map((n) => (
        <p key={n} className="mig-note">
          <Icon name="info" size={14} /> {n}
        </p>
      ))}
      {(status.errors ?? []).map((e) => (
        <p key={e} className="error-text">
          {e}
        </p>
      ))}
      {!status.running && (
        <p>
          {tx("Next, look through your {library}. When you're happy, stop the old apps. The guide has the best order.", { library: <Link to="/library">{t('Library')}</Link> })}
        </p>
      )}
    </fieldset>
  )
}
