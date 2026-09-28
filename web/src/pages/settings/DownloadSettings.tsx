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
  useEffect(loadSettings, [loadSettings])

  const torrentEnabled = settings?.torrentEnabled !== false
  const usenetOnly = settings?.defaultSources === 'usenet'
  const torrentOff = !torrentEnabled

  async function setTorrentEnabled(v: boolean) {
    try {
      const saved = await api.putSettings({ torrentEnabled: v })
      setSettings(saved)
      toast.success(v ? 'Torrent client turned on.' : 'Torrent client turned off. Nothing will be downloaded by torrent.')
      loadStatus()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="settings-stack">
      <fieldset className="group folders span-all">
        <legend>
          <Icon name="download" size={14} /> Where to download from
        </legend>
        <DefaultSourcesCard
          onChanged={async (value) => {
            // Choosing Usenet only also switches the torrent client off, and choosing torrents back on again.
            const wantTorrent = value !== 'usenet'
            if (wantTorrent !== (settings?.torrentEnabled !== false)) await setTorrentEnabled(wantTorrent)
            else loadSettings()
          }}
        />
      </fieldset>

      <fieldset className="group usenet">
        <legend>
          <Icon name="server" size={14} /> Usenet (NZB)
        </legend>
        <div className="group-cols">
          <section className="card">
            <h2>
              Downloader <span className="badge downloaded">built in</span>{' '}
              {status && <span className={`badge ${status.usenet.ready ? 'downloaded' : 'missing'}`}>{status.usenet.ready ? 'ready' : 'needs your provider'}</span>}
            </h2>
            <p style={{ color: 'var(--text-dim)' }}>
              The NZB downloader is part of Mediarium: it downloads, repairs (PAR2) and unpacks by itself, so there is nothing to install, no SABnzbd or NZBGet. The only thing it needs from you is your Usenet provider's login
              {status && !status.usenet.ready ? ', which you can add on the right.' : ' (added on the right).'}
            </p>
          </section>
          <UsenetServersSection onChange={loadStatus} />
        </div>
      </fieldset>

      <fieldset className={`group torrent${torrentOff ? ' disabled' : ''}`}>
        <legend>
          <Icon name="magnet" size={14} /> Torrents
        </legend>
        <Switch
          checked={torrentEnabled}
          onChange={(v) => void setTorrentEnabled(v)}
          label="Use the built-in torrent client"
          description={usenetOnly ? 'You chose Usenet only, so this is off. Turn it on if you also want torrents.' : 'Turn this off if you only use Usenet. The client stops and no torrents are searched or downloaded.'}
        />
        <fieldset disabled={torrentOff} className="plain-fieldset">
          <div className="group-cols" style={{ marginTop: 14 }}>
            <section className="card">
              <h2>
                Client <span className="badge downloaded">built in</span>{' '}
                {status && (
                  <span className={`badge ${torrentOff ? '' : status.torrent.ready ? 'downloaded' : 'missing'}`}>
                    {torrentOff ? 'off' : status.torrent.ready ? 'ready' : 'paused by your kill switch'}
                  </span>
                )}
              </h2>
              <p style={{ color: 'var(--text-dim)' }}>
                The torrent client is part of Mediarium too: nothing to install, no qBittorrent or Transmission, and no provider login. Torrents come from your torrent indexers under Settings &gt; Indexers.
                {status?.torrent.vpnRequired && (
                  <>
                    {' '}
                    You turned on the optional VPN kill switch, so torrents only run while a VPN is connected ({status.torrent.vpnConnected ? 'connected now' : 'not connected'}). <Link to="../vpn">VPN settings</Link>
                  </>
                )}
              </p>
              {status && !status.torrent.vpnConnected && !status.torrent.vpnRequired && !torrentOff && (
                <div className="notice notice-warn">
                  <strong>Recommended: use a VPN for torrents.</strong> Without one, everyone in a torrent swarm can see your home IP address. It is your choice and nothing is blocked. <Link to="../vpn">Set up a VPN</Link>
                </div>
              )}
            </section>
            <TorrentSection />
          </div>
        </fieldset>
      </fieldset>
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
        The default for every movie and show. You can change it per title when adding it or later on its page. When a Usenet and a
        torrent release are equally good, Usenet wins.
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

function serverLabel(s: UsenetServer): string {
  return s.priority === 0 ? 'Primary' : `Backup ${s.priority}`
}

function UsenetServersSection({ onChange }: { onChange: () => void }) {
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
    if (!window.confirm(`Remove the server "${s.name}"?`)) return
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
        This is the news-server login your Usenet provider gave you (a subscription, not a program). Mediarium's built-in downloader connects to it directly. You can add a second provider as a <em>backup</em> (a higher priority number, ideally on a different backbone) to fetch articles your main one is missing.
      </p>
      {error && <p className="error-text">{error}</p>}

      {servers !== null && servers.length === 0 && !editing && (
        <p>
          <strong>No Usenet provider added yet.</strong> Torrents work without one, but NZB downloads need your provider's login.
        </p>
      )}
      {servers !== null && servers.length > 0 && (
        <table style={{ marginBottom: 16 }}>
          <thead>
            <tr>
              <th>Provider</th>
              <th>Role</th>
              <th>Connections</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {servers.map((s) => (
              <tr key={s.id} style={s.enabled ? undefined : { opacity: 0.55 }}>
                <td>
                  <strong>{s.name}</strong>
                  <div style={{ color: 'var(--text-dim)', fontSize: '0.8rem', fontFamily: 'var(--font-mono)' }}>
                    {s.host}:{s.port} {s.useSsl ? '(SSL)' : '(no SSL)'}
                  </div>
                </td>
                <td>
                  <span className={`badge ${s.priority === 0 ? 'downloaded' : ''}`}>{serverLabel(s)}</span> {!s.enabled && <span className="badge">disabled</span>}
                </td>
                <td>{s.connections}</td>
                <td style={{ whiteSpace: 'nowrap' }}>
                  <TestButton run={() => api.testUsenetServer(s.id)} />{' '}
                  <button onClick={() => setEditing(edit(s))}>Edit</button>{' '}
                  <button onClick={() => toggle(s)}>{s.enabled ? 'Disable' : 'Enable'}</button>{' '}
                  <button onClick={() => remove(s)}>Remove</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
