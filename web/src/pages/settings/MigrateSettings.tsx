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
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { apiKey as apiKeyCheck, firstError, folderPath, required, url as urlCheck } from '../../validate'

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
    title: 'Movies and TV shows',
    apps: [
      { key: 'radarr', name: 'Radarr', what: 'your movies', port: '7878', help: 'Settings → General → Security → API Key', color: '#ffc230' },
      { key: 'sonarr', name: 'Sonarr', what: 'your TV shows', port: '8989', help: 'Settings → General → Security → API Key', color: '#35c5f4' },
      { key: 'medusa', name: 'Medusa', what: 'your TV shows', port: '8081', help: 'Config → General → Interface → API key', color: '#5b8def' },
      { key: 'sickchill', name: 'SickChill', what: 'your TV shows', port: '8081', help: 'Config → General → Interface → API key', color: '#8f6bd8' },
    ],
  },
  {
    title: 'Indexers',
    apps: [
      { key: 'prowlarr', name: 'Prowlarr', what: 'your indexers', port: '9696', help: 'Settings → General → Security → API Key', color: '#e66000' },
      { key: 'jackett', name: 'Jackett', what: 'your indexers', port: '9117', help: 'The API Key at the top of the Jackett page', color: '#c03a2b' },
      { key: 'nzbhydra', name: 'NZBHydra2', what: 'all your indexers as one', port: '5076', help: 'Config → Main → Security → API key', color: '#2a9d8f', hydra: true },
    ],
  },
  {
    title: 'Usenet downloaders',
    apps: [
      { key: 'sabnzbd', name: 'SABnzbd', what: 'your Usenet servers', port: '8080', help: 'Config (cog) → General → Security → API Key', color: '#f5c518' },
      { key: 'nzbget', name: 'NZBGet', what: 'your Usenet servers', port: '6789', help: 'The login you use for NZBGet (default nzbget / tegbzn6789)', color: '#3fa34d', auth: 'userpass' },
    ],
  },
  {
    title: 'Requests and subtitles',
    apps: [
      { key: 'overseerr', name: 'Overseerr / Jellyseerr', what: 'what people asked for', port: '5055', help: 'Settings → General → API Key', color: '#7c6bf0' },
      { key: 'ombi', name: 'Ombi', what: 'what people asked for', port: '3579', help: 'Settings → Configuration → API Key', color: '#e5813d' },
      { key: 'bazarr', name: 'Bazarr', what: 'your subtitle languages', port: '6767', help: 'Settings → General → Security → API Key', color: '#c94f9e' },
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

const ACTION_LABEL = { add: 'Will add', exists: 'Already here', skip: 'Skipped' } as const
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
    connErrors[`${a.key}.url`] = firstError(required(c.url, `Add the address of ${a.name}, for example ${example}.`), urlCheck(c.url, { example }))
    if (a.auth === 'userpass') connErrors[`${a.key}.username`] = required(c.username, `Add the username you use to sign in to ${a.name}.`)
    else connErrors[`${a.key}.apiKey`] = firstError(required(c.apiKey, `Add the API key from ${a.name}. The hint under the box says where to find it.`), apiKeyCheck(c.apiKey))
  }
  if (inUse.length === 0) connErrors.apps = 'Fill in the address and API key of at least one app first.'
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
    timer.current = setInterval(async () => {
      try {
        const st = await api.migrateStatus()
        setStatus(st)
        if (!st.running) clearInterval(timer.current)
      } catch {
        clearInterval(timer.current)
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
      toast.success(on ? 'Automatic searching is on again.' : 'Automatic searching paused.')
    } catch (e) {
      toast.error(errText(e))
    }
  }

  const titleApps: { key: 'radarr' | 'sonarr' | 'medusa' | 'sickchill'; label: string; toggle: 'movies' | 'series' }[] = [
    { key: 'radarr', label: 'Movies from Radarr', toggle: 'movies' },
    { key: 'sonarr', label: 'TV shows from Sonarr', toggle: 'series' },
    { key: 'medusa', label: 'TV shows from Medusa', toggle: 'series' },
    { key: 'sickchill', label: 'TV shows from SickChill', toggle: 'series' },
  ]
  const rootFolders = titleApps.flatMap((t) => (preview?.[t.key]?.rootFolders ?? []).map((r) => ({ ...r, from: t.label })))
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
        Already using Radarr, Sonarr, Prowlarr, SABnzbd or similar? Bring your movies, shows, indexers, Usenet servers, requests and subtitle languages over.
        Those apps are only read, never changed, and your files stay where they are.{' '}
        <a href={`${DOCS_URL}/migrate.md`} target="_blank" rel="noreferrer">
          Step-by-step guide
        </a>
      </p>

      {/* 1. Connect */}
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="key" size={14} /> 1. Connect your current apps
        </legend>
        {GROUPS.map((g) => (
          <div key={g.title} className="mig-group">
            <h3>{g.title}</h3>
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
                      Address
                      <input value={c.url} onChange={(e) => set({ url: e.target.value })} placeholder={`http://192.168.1.10:${a.port}`} {...v.bind(`${a.key}.url`, c.url, (t) => set({ url: t }))} />
                      <FieldError v={v} name={`${a.key}.url`} />
                    </label>
                    {a.auth === 'userpass' ? (
                      <div className="mig-two">
                        <label>
                          Username
                          <input value={c.username} onChange={(e) => set({ username: e.target.value })} autoComplete="off" {...v.bind(`${a.key}.username`, c.username, (t) => set({ username: t }))} />
                          <FieldError v={v} name={`${a.key}.username`} />
                        </label>
                        <label>
                          Password
                          <input type="password" value={c.password} onChange={(e) => set({ password: e.target.value })} autoComplete="new-password" />
                        </label>
                      </div>
                    ) : (
                      <label>
                        API key
                        <input value={c.apiKey} onChange={(e) => set({ apiKey: e.target.value })} autoComplete="off" {...v.bind(`${a.key}.apiKey`, c.apiKey, (t) => set({ apiKey: t }))} />
                        <FieldError v={v} name={`${a.key}.apiKey`} />
                      </label>
                    )}
                    <small>{a.help}</small>
                    {a.hydra && (
                      <label className="mig-check">
                        <input type="checkbox" checked={c.torznab} onChange={(e) => set({ torznab: e.target.checked })} /> It also has torrent indexers (add its torrent feed too)
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
            <Icon name="search" size={15} /> {busy && !status?.running ? 'Reading your apps…' : 'Check and preview'}
          </button>
          <small style={{ color: 'var(--text-dim)', alignSelf: 'center' }}>Fill in the ones you use and leave the rest empty. Nothing is imported yet.</small>
        </div>
        <FieldError v={v} name="apps" />
        {inUse.length > 0 && <FormProblem v={v} verb="check them" />}
        {error && <p className="error-text">{error}</p>}
      </fieldset>

      {preview && (
        <>
          {/* 2. Folders */}
          {rootFolders.length > 0 && (
            <fieldset className="group usenet span-all">
              <legend>
                <Icon name="folder" size={14} /> 2. Check your folders
              </legend>
              <p style={{ marginTop: 0, color: 'var(--text-dim)' }}>
                Your other apps may see your folders under different paths than Mediarium ({preview.moviesPath} for movies, {preview.tvPath} for TV). Point each of their
                folders at the same place here. Green means found.
              </p>
              <table className="mig-roots">
                <thead>
                  <tr>
                    <th>App</th>
                    <th>Their folder</th>
                    <th>Same folder in Mediarium</th>
                    <th>Found</th>
                  </tr>
                </thead>
                <tbody>
                  {rootFolders.map((r, i) => {
                    const idx = pathMap.findIndex((m) => m.from === r.path)
                    return (
                      <tr key={`${r.from}-${r.path}-${i}`}>
                        <td>
                          <small>{r.from.replace(/^(Movies|TV shows) from /, '')}</small>
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
                          {r.suggested && <small className="mig-hint">Suggested</small>}
                        </td>
                        <td>
                          <span className={`badge ${r.exists && r.foldersFound > 0 ? 'downloaded' : 'failed'}`}>
                            {r.foldersFound} of {r.titles} title folders
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
                Check again with these folders
              </button>
              <FormProblem v={vf} verb="check again" />
            </fieldset>
          )}

          {/* 3. What will happen */}
          <fieldset className="group torrent span-all">
            <legend>
              <Icon name="list" size={14} /> 3. What will be imported
            </legend>
            <div className="mig-summary">
              {titleApps.map((t) => {
                const p = preview[t.key]
                return p ? <TitlesCard key={t.key} label={t.label} p={p} on={include[t.toggle]} onToggle={(v) => setInclude({ ...include, [t.toggle]: v })} /> : null
              })}
              {(['prowlarr', 'jackett', 'nzbhydra'] as const).map((k) => {
                const p = preview[k]
                if (!p) return null
                const name = ALL_APPS.find((a) => a.key === k)!.name
                return (
                  <SimpleCard key={k} label={`Indexers from ${name}`} ok={p.ok} error={p.error} summary={p.summary} on={include.indexers} onToggle={(v) => setInclude({ ...include, indexers: v })}>
                    {p.items.map((i) => (
                      <li key={`${i.name}-${i.baseUrl ?? ''}`}>
                        <span className={`mig-act act-${i.action}`}>{ACTION_LABEL[i.action]}</span> {i.name} <small>{i.protocol}</small>
                        {i.reason && <small> · {i.reason}</small>}
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
                  <SimpleCard key={k} label={`Usenet servers from ${name}`} ok={p.ok} error={p.error} summary={p.summary} on={include.usenetServers} onToggle={(v) => setInclude({ ...include, usenetServers: v })}>
                    {p.items.map((s) => (
                      <li key={`${s.host}-${s.username}`}>
                        <span className={`mig-act act-${s.action}`}>{ACTION_LABEL[s.action]}</span> {s.name || s.host} <small>{s.priority === 0 ? 'main' : `backup ${s.priority}`}</small>
                        {s.reason && <small> · {s.reason}</small>}
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
                  <SimpleCard key={k} label={`Requests from ${name}`} ok={p.ok} error={p.error} summary={p.summary} on={include.requests} onToggle={(v) => setInclude({ ...include, requests: v })}>
                    {p.items.map((r) => (
                      <li key={`${r.mediaType}-${r.tmdbId ?? r.tvdbId}-${r.title}`}>
                        <span className={`mig-act act-${r.action}`}>{ACTION_LABEL[r.action]}</span> {r.title} {r.year ? <small>({r.year})</small> : null}
                        <small>
                          {' '}
                          · {r.mediaType === 'movie' ? 'movie' : 'show'} · {r.status}
                          {r.requestedBy.length ? ` · asked by ${r.requestedBy.join(', ')}` : ''}
                          {r.reason ? ` · ${r.reason}` : ''}
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
                <strong>Quality profiles</strong>
                <table>
                  <tbody>
                    {profileRows.map((pr) => (
                      <tr key={`${pr.app}-${pr.arrId}`}>
                        <td>
                          {pr.app === 'radarr' ? 'Radarr' : 'Sonarr'}: <strong>{pr.arrName}</strong> <small>({pr.titles} titles)</small>
                        </td>
                        <td>→</td>
                        <td>
                          <select value={profileMapping[pr.arrName] ?? pr.profileId} onChange={(e) => setProfileMapping({ ...profileMapping, [pr.arrName]: Number(e.target.value) })}>
                            <option value={0}>Default profile</option>
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
              <Icon name="download" size={14} /> 4. Import
            </legend>
            <p style={{ marginTop: 0 }}>
              Files stay where they are, and nothing is moved, copied or downloaded. Pause automatic searching until you switch over, so your old apps and Mediarium don&apos;t grab the same title.
            </p>
            {automationOn !== null && (
              <Switch
                checked={!automationOn}
                onChange={(v) => void setAutomation(!v)}
                label="Pause automatic searching while I test"
                description="Nothing searches or downloads on its own until you turn this off."
                showState
              />
            )}
            <div className="form-actions" style={{ marginTop: 14 }}>
              <button className="primary btn-with-icon big" onClick={() => void run()} disabled={busy || !!status?.running}>
                <Icon name="download" size={16} /> {status?.running ? 'Importing…' : 'Import now'}
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
      <span className="act-add">{s.add} to add</span>
      <span className="act-exists">{s.exists} already here</span>
      <span className="act-skip">{s.skip} skipped</span>
    </div>
  )
}

function TitlesCard({ label, p, on, onToggle }: { label: string; p: MigrateTitlesPreview; on: boolean; onToggle: (v: boolean) => void }) {
  const [open, setOpen] = useState(false)
  if (!p.ok)
    return (
      <div className="mig-card bad">
        <strong>{label}</strong>
        <p className="error-text">{p.error}</p>
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
      {skipped.length > 0 && <small className="mig-hint">Skipped usually means the folder wasn&apos;t found. Check step 2.</small>}
      <button className="btn-sm" onClick={() => setOpen((v) => !v)}>
        {open ? 'Hide the list' : `Show all ${p.items.length}`}
      </button>
      {open && (
        <ul className="mig-list">
          {p.items.map((i) => (
            <li key={`${i.tmdbId ?? i.tvdbId}-${i.title}`}>
              <span className={`mig-act act-${i.action}`}>{ACTION_LABEL[i.action]}</span> {i.title} {i.year ? <small>({i.year})</small> : null}
              <small>
                {' '}
                · {i.files} file{i.files === 1 ? '' : 's'}
                {i.quality ? ` · ${i.quality}` : ''}
                {i.reason ? ` · ${i.reason}` : ''}
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
        {open ? 'Hide the list' : 'Show the list'}
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
        <strong>Subtitle languages from Bazarr</strong>
        <p className="error-text">{p.error}</p>
      </div>
    )
  const noEquivalent = (p.providers ?? []).filter((x) => !x.equivalent)
  return (
    <div className={`mig-card${on ? '' : ' off'}`}>
      <label className="mig-card-head">
        <input type="checkbox" checked={on} onChange={(e) => onToggle(e.target.checked)} /> <strong>Subtitle languages from Bazarr</strong>
        {p.version && <small>v{p.version}</small>}
      </label>
      <div className="mig-counts">
        {p.languages.map((l) => (
          <span key={l.code} className={l.action === 'skip' ? 'act-skip' : 'act-add'} title={l.reason}>
            {l.name}
          </span>
        ))}
      </div>
      <small className="mig-hint">Replaces the subtitle languages in Settings → Info, lists and subtitles → Subtitles.</small>
      {noEquivalent.length > 0 && (
        <>
          <button className="btn-sm" onClick={() => setOpen((v) => !v)}>
            {open ? 'Hide' : `${noEquivalent.length} Bazarr subtitle sources have no equivalent here`}
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
    ['Movies', r.movies],
    ['TV shows', r.series],
    ['Indexers', r.indexers],
    ['Usenet servers', r.usenetServers],
    ['Requests', r.requests],
    ['Subtitle languages', r.subtitleLanguages],
  ]
  return (
    <fieldset className="group folders span-all">
      <legend>
        <Icon name="activity" size={14} /> {status.running ? 'Importing…' : status.step === 'failed' ? 'Import stopped' : 'Import finished'}
      </legend>
      <div className="mig-progress">
        <div style={{ width: `${pct}%` }} />
      </div>
      <small style={{ color: 'var(--text-dim)' }}>
        {status.running ? `${status.step}: ${status.done} of ${status.total}` : status.finishedAt ? `Finished ${new Date(status.finishedAt).toLocaleString()}` : ''}
      </small>
      <div className="mig-summary" style={{ marginTop: 12 }}>
        {rows
          .filter(([, c]) => c)
          .map(([label, c]) => (
            <div key={label} className="mig-card">
              <strong>{label}</strong>
              <div className="mig-counts">
                <span className="act-add">{c!.added} added</span>
                <span className="act-exists">{c!.existing} already here</span>
                {c!.skipped > 0 && <span className="act-skip">{c!.skipped} skipped</span>}
                {c!.failed > 0 && <span className="act-fail">{c!.failed} failed</span>}
              </div>
              {c!.filesLinked > 0 && <small>{c!.filesLinked} existing files linked</small>}
              {c!.disabled > 0 && <small> · {c!.disabled} added but switched off, so check them</small>}
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
          Next, look through your <Link to="/library">Library</Link>. When you&apos;re happy, stop the old apps. The guide has the best order.
        </p>
      )}
    </fieldset>
  )
}
