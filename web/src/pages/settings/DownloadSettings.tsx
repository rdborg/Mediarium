import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type DownloadsStatus, type Settings, type UsenetServer } from '../../api'
import Icon from '../../components/Icon'
import Switch from '../../components/Switch'
import UsenetServerForm, { blankServer, type UsenetServerDraft } from '../../components/UsenetServerForm'
import TestButton from '../../components/TestButton'
import { useToast } from '../../components/Toast'
import { useAutosaveSetting } from '../../useAutosave'
import TorrentSection from './TorrentSection'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'

// Downloads: Mediarium downloads for itself. Usenet needs only your
// provider's news-server account; torrents need nothing at all. There is no
// separate download client program to install or connect (unlike Sonarr and
// Radarr, which hand grabs to SABnzbd, qBittorrent and the like).
export default function DownloadSettings() {
  const toast = useToast()
  const [status, setStatus] = useState<DownloadsStatus | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const loadStatus = useCallback(() => {
    api.downloadsStatus().then(setStatus).catch(() => undefined)
  }, [])
  const loadSettings = useCallback(() => {
    api.getSettings().then(setSettings).catch(() => undefined)
  }, [])
  useEffect(loadStatus, [loadStatus])
  useLive(loadStatus, 10000)
  useEffect(loadSettings, [loadSettings])

  const torrentEnabled = settings?.torrentEnabled !== false
  const usenetOnly = settings?.defaultSources === 'usenet'
  const torrentOff = !torrentEnabled

  // One click from a blurred section's notice: which sources to use.
  async function useSources(value: 'both' | 'usenet' | 'torrent') {
    try {
      const saved = await api.putSettings({ defaultSources: value, torrentEnabled: value !== 'usenet' })
      setSettings(saved)
      toast.success(value === 'both' ? 'Now downloading from Usenet and torrents.' : value === 'usenet' ? 'Now downloading from Usenet only.' : 'Now downloading from torrents only.')
      loadStatus()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  const torrentOnly = settings?.defaultSources === 'torrent'

  async function setTorrentEnabled(v: boolean) {
    try {
      const saved = await api.putSettings({ torrentEnabled: v })
      setSettings(saved)
      toast.success(v ? 'Torrent client turned on.' : 'Torrent client turned off.')
      loadStatus()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <div className="half-cols span-all">
      <fieldset className="group folders">
        <legend>
          <Icon name="download" size={14} /> Where to download from
        </legend>
        <DefaultSourcesCard
          key={settings?.defaultSources ?? 'loading'}
          onChanged={async (value) => {
            // Choosing Usenet only also switches the torrent client off, and choosing torrents back on again.
            const wantTorrent = value !== 'usenet'
            if (wantTorrent !== (settings?.torrentEnabled !== false)) await setTorrentEnabled(wantTorrent)
            else loadSettings()
          }}
        />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="list" size={14} /> Downloads at the same time
        </legend>
        <DownloadsAtOnceCard />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="download" size={14} /> Speed and space
        </legend>
        <SpeedAndSpaceCard />
      </fieldset>
      </div>

      <div className="half-cols span-all">
      <fieldset className={`group usenet${torrentOnly ? ' section-off' : ''}`}>
        <legend>
          <Icon name="server" size={14} /> Usenet (NZB)
        </legend>
        {torrentOnly && (
          <SectionOffNotice
            title="Usenet is not in use"
            text="You chose torrents only, so Usenet is skipped."
            actions={[
              ['Use Usenet too', () => void useSources('both')],
              ['Usenet only', () => void useSources('usenet')],
            ]}
          />
        )}
        <div className="stack-cols section-body">
          <section className="card">
            <h2>
              Downloader <span className="badge downloaded">built in</span>{' '}
              {status && <span className={`badge ${status.usenet.ready ? 'downloaded' : 'missing'}`}>{status.usenet.ready ? 'ready' : 'needs your provider'}</span>}
            </h2>
            <p style={{ color: 'var(--text-dim)' }}>
              Built in, so there's no SABnzbd or NZBGet to set up. It only needs your Usenet provider's login{status && !status.usenet.ready ? ', which you add on the right.' : '.'}
            </p>
          </section>
          <UsenetServersSection onChange={loadStatus} />
        </div>
      </fieldset>

      <fieldset className={`group torrent${torrentOff ? ' disabled section-off' : ''}`}>
        <legend>
          <Icon name="magnet" size={14} /> Torrents
        </legend>
        {torrentOff && (
          <SectionOffNotice
            title="Torrents are switched off"
            text={usenetOnly ? 'You chose Usenet only, so torrents are skipped.' : 'The torrent client is stopped.'}
            actions={[
              ['Use torrents too', () => void useSources('both')],
              ['Torrents only', () => void useSources('torrent')],
            ]}
          />
        )}
        <div className="section-body">
        <Switch
          checked={torrentEnabled}
          onChange={(v) => void setTorrentEnabled(v)}
          label="Use the built-in torrent client"
          description={usenetOnly ? 'Off because you chose Usenet only.' : 'Turn it off if you only use Usenet.'}
          showState
        />
        <fieldset disabled={torrentOff} className="plain-fieldset">
          <div className="stack-cols" style={{ marginTop: 14 }}>
            <section className="card">
              <h2>
                Client <span className="badge downloaded">built in</span>{' '}
                {status && (
                  <span className={`badge ${torrentOff ? '' : status.torrent.ready ? 'downloaded' : 'missing'}`}>
                    {torrentOff ? 'off' : status.torrent.ready ? 'ready' : 'waiting for the VPN'}
                  </span>
                )}
              </h2>
              <p style={{ color: 'var(--text-dim)' }}>
                Built in too, so there's nothing to install and no login. Torrents come from the indexers you add under Settings &gt; Indexers &amp; Search.
                {status?.torrent.vpnRequired && (
                  <>
                    {' '}
                    The VPN kill switch is on, so torrents only run while a VPN is connected ({status.torrent.vpnConnected ? 'connected now' : 'not connected'}). <Link to="../vpn">VPN settings</Link>
                  </>
                )}
                {status?.torrent.blockedByVpn && !status.torrent.vpnRequired && (
                  <>
                    {' '}
                    Your VPN is on but not connected, so torrents wait for it. <Link to="../vpn">VPN settings</Link>
                  </>
                )}
              </p>
              {status && status.torrent.vpnState === 'off' && !status.torrent.vpnRequired && !torrentOff && (
                <div className="notice notice-warn">
                  <strong>Use a VPN for torrents.</strong> Without one, everyone in the swarm can see your home IP address. <Link to="../vpn">Set up a VPN</Link>
                </div>
              )}
            </section>
            <TorrentSection status={status?.torrent ?? null} />
          </div>
        </fieldset>
        </div>
      </fieldset>
      </div>
    </div>
  )
}

// Which downloaders new movies and shows may use unless you say otherwise
// when adding them (or later on the item's own page).
function DefaultSourcesCard({ onChanged }: { onChanged: (value: string) => void | Promise<void> }) {
  const sources = useAutosaveSetting<string>(
    (s) => s.defaultSources ?? 'both',
    (v) => ({ defaultSources: v as 'both' | 'usenet' | 'torrent' }),
    (saved) => `Saved: new titles download from ${saved === 'both' ? 'Usenet and torrents' : saved === 'usenet' ? 'Usenet only' : 'torrents only'}.`,
    'both',
  )
  return (
    <div>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        Used for every movie and show unless you change it on the title. Usenet wins when both are equally good.
      </p>
      <div className="seg" role="radiogroup" aria-label="Default downloaders">
        {[
          ['both', 'Usenet and torrents'],
          ['usenet', 'Usenet only'],
          ['torrent', 'Torrents only'],
        ].map(([value, label]) => (
          <button
            key={value}
            role="radio"
            aria-checked={sources.value === value}
            className={sources.value === value ? 'active' : ''}
            disabled={!sources.loaded || sources.saving}
            onClick={() => void sources.change(value).then(() => onChanged(value))}
          >
            {label}
          </button>
        ))}
      </div>
    </div>
  )
}

// How many downloads run together. Usenet and torrents share this number: the
// rest wait in line, and the next one starts by itself when one is finished
// (downloaded, repaired, unpacked and moved into your library).
function DownloadsAtOnceCard() {
  const atOnce = useAutosaveSetting<number>(
    (s) => s.downloadsAtOnce ?? 1,
    (v) => ({ downloadsAtOnce: v }),
    (saved) => (saved === 1 ? 'Saved: one download at a time.' : `Saved: up to ${saved} downloads at the same time.`),
    1,
  )
  return (
    <div>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        Usenet and torrents share this number. The rest wait in line.
      </p>
      <div className="seg" role="radiogroup" aria-label="Downloads at the same time">
        {[1, 2, 3, 4, 5].map((n) => (
          <button key={n} role="radio" aria-checked={atOnce.value === n} className={atOnce.value === n ? 'active' : ''} disabled={!atOnce.loaded || atOnce.saving} onClick={() => void atOnce.change(n)}>
            {n}
          </button>
        ))}
      </div>
    </div>
  )
}

const SPEEDS = [0, 1, 2, 5, 10, 20, 50, 100]
const FREE = [0, 2, 5, 10, 20, 50]
const HOURS = Array.from({ length: 24 }, (_, h) => h)

// A download speed limit (all day, or only between two hours) and how much
// free space to keep. Each change saves by itself.
function SpeedAndSpaceCard() {
  const limit = useAutosaveSetting<number>(
    (s) => s.speedLimitMB ?? 0,
    (v) => ({ speedLimitMB: v }),
    (v) => (v === 0 ? 'Saved: no speed limit.' : `Saved: downloads are limited to ${v} MB/s.`),
    0,
  )
  const hours = useAutosaveSetting<string>(
    (s) => s.speedLimitHours ?? '',
    (v) => ({ speedLimitHours: v }),
    (v) => (v === '' ? 'Saved: the limit applies all day.' : `Saved: the limit applies from ${v.split('-')[0]}:00 to ${v.split('-')[1]}:00.`),
    '',
  )
  const free = useAutosaveSetting<number>(
    (s) => s.minFreeGB ?? 5,
    (v) => ({ minFreeGB: v }),
    (v) => (v === 0 ? 'Saved: downloads start whatever the free space.' : `Saved: no new download starts with less than ${v} GB free.`),
    5,
  )
  const [from, to] = hours.value ? hours.value.split('-').map(Number) : [8, 23]
  const busy = !limit.loaded || limit.saving || hours.saving || free.saving
  return (
    <div>
      <label className="inline-field">
        Limit download speed to
        <select value={limit.value} disabled={busy} onChange={(e) => void limit.change(Number(e.target.value))}>
          {SPEEDS.map((n) => (
            <option key={n} value={n}>
              {n === 0 ? 'no limit' : `${n} MB/s`}
            </option>
          ))}
        </select>
      </label>
      {limit.value > 0 && (
        <div className="inline-field" style={{ marginTop: 10 }}>
          <label className="inline-field">
            <input type="checkbox" checked={hours.value !== ''} disabled={busy} onChange={(e) => void hours.change(e.target.checked ? '8-23' : '')} />
            Only from
          </label>
          <select aria-label="From" value={from} disabled={busy || hours.value === ''} onChange={(e) => void hours.change(`${e.target.value}-${to === Number(e.target.value) ? (to + 1) % 24 : to}`)}>
            {HOURS.map((h) => (
              <option key={h} value={h}>
                {h}:00
              </option>
            ))}
          </select>
          to
          <select aria-label="To" value={to} disabled={busy || hours.value === ''} onChange={(e) => void hours.change(`${from === Number(e.target.value) ? (from + 23) % 24 : from}-${e.target.value}`)}>
            {HOURS.map((h) => (
              <option key={h} value={h}>
                {h}:00
              </option>
            ))}
          </select>
        </div>
      )}
      <p className="field-hint">Usenet and torrents share the limit. Outside the hours you choose, downloads run at full speed.</p>
      <label className="inline-field" style={{ marginTop: 14 }}>
        Keep at least
        <select value={free.value} disabled={busy} onChange={(e) => void free.change(Number(e.target.value))}>
          {FREE.map((n) => (
            <option key={n} value={n}>
              {n === 0 ? 'no minimum' : `${n} GB`}
            </option>
          ))}
        </select>
        free in the downloads folder
      </label>
      <p className="field-hint">With less free space than this, no new download starts. They wait and start by themselves once there is room.</p>
    </div>
  )
}

// Shown over a blurred Usenet or torrent section that is not in use.
function SectionOffNotice({ title, text, actions }: { title: string; text: string; actions: [string, () => void][] }) {
  return (
    <div className="section-off-notice" role="note">
      <strong>{title}</strong>
      <p>{text}</p>
      <div className="row-actions">
        {actions.map(([label, run], i) => (
          <button key={label} className={i === 0 ? 'primary' : ''} onClick={run}>
            {label}
          </button>
        ))}
      </div>
    </div>
  )
}

function serverLabel(s: UsenetServer): string {
  return s.priority === 0 ? 'Primary' : `Backup ${s.priority}`
}

function UsenetServersSection({ onChange }: { onChange: () => void }) {
  const confirm = useConfirm()
  const [servers, setServers] = useState<UsenetServer[] | null>(null)
  const [editing, setEditing] = useState<UsenetServerDraft | null>(null)
  const toast = useToast()
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    api
      .listUsenetServers()
      .then(setServers)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    onChange()
  }, [onChange])
  useEffect(reload, [reload])
  useLive(reload, 15000)

  async function save(d: UsenetServerDraft) {
    setError('')
    try {
      const body = {
        name: d.name,
        host: d.host.trim(),
        port: d.port,
        useSsl: d.useSsl,
        username: d.username,
        password: d.password,
        connections: d.connections,
        priority: d.priority,
        enabled: d.enabled,
      }
      if (d.id !== undefined) await api.updateUsenetServer(d.id, body)
      else await api.createUsenetServer(body)
      setEditing(null)
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggle(s: UsenetServer) {
    try {
      await api.updateUsenetServer(s.id, { ...s, password: '', enabled: !s.enabled })
      toast.success(s.enabled ? `${s.name} disabled.` : `${s.name} enabled.`)
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(s: UsenetServer) {
    if (!(await confirm({ title: `Remove the server "${s.name}"?`, body: <p>Downloads stop using it. You can add it again later.</p>, confirmLabel: 'Remove server', danger: true }))) return
    try {
      await api.deleteUsenetServer(s.id)
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const edit = (s: UsenetServer): UsenetServerDraft => ({
    id: s.id,
    name: s.name,
    host: s.host,
    port: s.port,
    useSsl: s.useSsl,
    username: s.username ?? '',
    password: '',
    connections: s.connections,
    priority: s.priority,
    enabled: s.enabled,
    hasPassword: s.hasPassword,
  })

  return (
    <section className="card">
      <h2>Your Usenet provider</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        The login your Usenet provider gave you. Add a second provider as a <em>backup</em>, ideally on a different backbone, to fill in articles the first one is missing.
      </p>
      {error && <p className="error-text">{error}</p>}

      {servers !== null && servers.length === 0 && !editing && (
        <p>
          <strong>No Usenet provider yet.</strong> NZB downloads need one, torrents don't.
        </p>
      )}
      {servers !== null && servers.length > 0 && (
        <div className="server-list">
          {servers.map((s) => (
            <div key={s.id} className="server-row" style={s.enabled ? undefined : { opacity: 0.6 }}>
              <div className="server-info">
                <div className="server-name">
                  <strong>{s.name}</strong>
                  <span className={`badge ${s.priority === 0 ? 'downloaded' : ''}`}>{serverLabel(s)}</span>
                  {!s.enabled && <span className="badge">disabled</span>}
                </div>
                <div className="server-host">
                  {s.host}:{s.port} {s.useSsl ? '(SSL)' : '(no SSL)'} · {s.connections} {s.connections === 1 ? 'connection' : 'connections'}
                </div>
              </div>
              <div className="server-actions">
                <TestButton run={() => api.testUsenetServer(s.id)} />
                <button onClick={() => setEditing(edit(s))}>Edit</button>
                <button onClick={() => toggle(s)}>{s.enabled ? 'Disable' : 'Enable'}</button>
                <button onClick={() => remove(s)}>Remove</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing ? (
        <div>
          <h3 style={{ marginTop: 0 }}>{editing.id !== undefined ? `Edit ${editing.name || 'server'}` : 'Add your Usenet provider'}</h3>
          <UsenetServerForm
            key={editing.id ?? 'new'}
            initial={editing}
            submitLabel={editing.id !== undefined ? 'Save server' : 'Add server'}
            onSubmit={save}
            onCancel={() => setEditing(null)}
          />
        </div>
      ) : (
        <button className="primary" onClick={() => setEditing(blankServer(servers && servers.length > 0 ? Math.max(...servers.map((s) => s.priority)) + 1 : 0))}>
          {servers && servers.length > 0 ? 'Add a backup provider…' : 'Add your Usenet provider…'}
        </button>
      )}
    </section>
  )
}
