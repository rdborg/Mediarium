import { useEffect, useState } from 'react'
import Switch from '../../components/Switch'
import { useAutosaveSetting } from '../../useAutosave'
import { api, type VPNConfig } from '../../api'
import { useLive } from '../../useLive'

// Named providers are a convenience wrapper, not a separate integration —
// every one of these still reduces to "paste a generic WireGuard config"
// (internal/vpn/tunnel.go never special-cases a provider). None of these
// providers expose a way to fetch a config automatically without the user
// handing us their account credentials, which this app deliberately never
// asks for — so this is honest guidance (a real, verified link + what to
// expect) rather than actual automation. Real links, checked against each
// provider's own current docs rather than guessed.
const VPN_PROVIDERS = [
  {
    id: 'mullvad',
    name: 'Mullvad',
    helpUrl: 'https://mullvad.net/en/account/wireguard-config',
    note: 'Log in with your account number, generate a key, choose a server, and download the config for "Linux" — then paste its fields below.',
  },
  {
    id: 'protonvpn',
    name: 'ProtonVPN',
    helpUrl: 'https://account.protonvpn.com',
    note: 'Sign in, go to Downloads → WireGuard configuration, generate one, then paste its fields below.',
  },
  {
    id: 'pia',
    name: 'Private Internet Access',
    helpUrl: 'https://github.com/pia-foss/manual-connections',
    note: "PIA doesn't have a simple web config page — their official script (linked) generates a WireGuard config using your account credentials, run on your own machine. Run it, then paste the resulting fields below.",
  },
  {
    id: 'surfshark',
    name: 'Surfshark',
    helpUrl: 'https://support.surfshark.com/hc/en-us/sections/13956254884754-How-to-set-up-WireGuard-manual-connection',
    note: 'Sign in at my.surfshark.com, go to VPN → Manual setup → WireGuard, generate one, then paste its fields below.',
  },
  {
    id: 'nordvpn',
    name: 'NordVPN',
    helpUrl: 'https://my.nordaccount.com/dashboard/nordvpn/manual-configuration/',
    note: 'NordVPN requires generating an access token first, then using it to fetch your WireGuard private key — see their page for the exact steps, then paste the resulting fields below.',
  },
  {
    id: 'custom',
    name: 'Custom / other provider',
    helpUrl: '',
    note: 'Works for any provider, or your own WireGuard server — paste the config fields below.',
  },
] as const

function VPNSection() {
  const [list, setList] = useState<VPNConfig[]>([])
  const [status, setStatus] = useState<{ connected: boolean; label?: string } | null>(null)
  const [providerId, setProviderId] = useState<string>(VPN_PROVIDERS[0].id)
  const [label, setLabel] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [peerPublicKey, setPeerPublicKey] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [localAddress, setLocalAddress] = useState('')
  const [error, setError] = useState('')
  const kill = useAutosaveSetting<boolean>(
    (s) => !!s.requireVpnForTorrents,
    (v) => ({ requireVpnForTorrents: v }),
    (saved) => (saved ? 'Saved: torrents now only run while a VPN is connected.' : 'Saved: torrents no longer require a VPN.'),
    false,
  )
  const [egressIp, setEgressIp] = useState('')
  const [egressIpStatus, setEgressIpStatus] = useState('')

  function reload() {
    api.listVPNConfigs().then(setList).catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.vpnStatus().then(setStatus).catch(() => {})
  }
  useEffect(reload, [])
  useLive(reload, 10000)

  const provider = VPN_PROVIDERS.find((p) => p.id === providerId) ?? VPN_PROVIDERS[0]

  async function add() {
    try {
      await api.createVPNConfig({
        label,
        provider: providerId,
        privateKey,
        peerPublicKey,
        endpoint,
        localAddresses: [localAddress],
      })
      setLabel('')
      setPrivateKey('')
      setPeerPublicKey('')
      setEndpoint('')
      setLocalAddress('')
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function activate(id: number) {
    setError('')
    try {
      await api.activateVPNConfig(id)
      setEgressIp('')
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function deactivate() {
    await api.deactivateVPN()
    setEgressIp('')
    reload()
  }

  async function remove(id: number) {
    await api.deleteVPNConfig(id)
    reload()
  }



  async function checkEgressIp() {
    setEgressIpStatus('Checking…')
    setEgressIp('')
    try {
      const { ip } = await api.vpnEgressIp()
      setEgressIp(ip)
      setEgressIpStatus('')
    } catch (e) {
      setEgressIpStatus(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <>
      <section className="card">
        <h2>VPN status</h2>
        <p style={{ color: 'var(--text-dim)' }}>
          Built-in WireGuard tunnel: no separate container or elevated privileges needed. It carries only torrent traffic.
        </p>
        {error && <p className="error-text">{error}</p>}
        {status && (
          <p>
            Status:{' '}
            <span className={`badge ${status.connected ? 'downloaded' : 'missing'}`}>
              {status.connected ? `Connected (${status.label})` : 'Disconnected'}
            </span>
            {status.connected && (
              <button style={{ marginLeft: 8 }} onClick={deactivate}>
                Disconnect
              </button>
            )}
          </p>
        )}
        {status?.connected && (
          <p style={{ fontSize: '0.9rem' }}>
            Egress IP: {egressIp || '—'} <button onClick={checkEgressIp}>Check egress IP</button>{' '}
            {egressIpStatus && <span style={{ color: 'var(--text-dim)' }}>{egressIpStatus}</span>}
          </p>
        )}
        {!status?.connected && (
          <div className="notice notice-warn">
            <strong>No VPN connected.</strong> Torrents still work, but everyone in a torrent swarm can see your home IP
            address, and your internet provider can see that you are torrenting. A VPN hides both. Usenet downloads are
            encrypted end to end with SSL and don't expose you this way. Using a VPN is your choice; Mediarium only
            recommends it.
          </div>
        )}
        <Switch
          checked={kill.value}
          onChange={kill.change}
          disabled={!kill.loaded || kill.saving}
          label="Kill switch (optional): only run torrents while a VPN is connected"
          description={
            <>
              Off by default, and saved as soon as you flip it. When on, a torrent grab waits and then fails rather than
              connect without the tunnel, and torrent traffic then uses only the tunnel (DHT and uTP are turned off, since
              they need a real UDP socket that would leak your address).
            </>
          }
        />
      </section>

      <section className="card grid-form">
        <h2>Add a VPN connection</h2>
        <label>
          Provider
          <select value={providerId} onChange={(e) => setProviderId(e.target.value)}>
            {VPN_PROVIDERS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: 0 }}>
          {provider.note}{' '}
          {provider.helpUrl && (
            <a href={provider.helpUrl} target="_blank" rel="noreferrer">
              Open {provider.name}'s config page ↗
            </a>
          )}{' '}
          Every provider ultimately reduces to the same WireGuard fields below.
        </p>
        <label>
          Label
          <input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="My VPN" />
        </label>
        <label>
          Endpoint
          <input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="vpn.example.com:51820" />
        </label>
        <label>
          Private key
          <input value={privateKey} onChange={(e) => setPrivateKey(e.target.value)} placeholder="base64 private key" />
        </label>
        <label>
          Peer public key
          <input value={peerPublicKey} onChange={(e) => setPeerPublicKey(e.target.value)} placeholder="base64 public key" />
        </label>
        <label>
          Local tunnel address
          <input value={localAddress} onChange={(e) => setLocalAddress(e.target.value)} placeholder="10.2.0.2/32" />
        </label>
        <div>
          <button className="primary" onClick={add} disabled={!label || !privateKey || !peerPublicKey || !endpoint || !localAddress}>
            Add config
          </button>
        </div>
      </section>

      <section className="card">
        <h2>Your VPN connections</h2>
        {list.length === 0 ? (
          <p style={{ color: 'var(--text-dim)' }}>None yet. Add one on the right to connect a tunnel for torrent traffic.</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Label</th>
                <th>Provider</th>
                <th>Endpoint</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {list.map((c) => (
                <tr key={c.id}>
                  <td>
                    {c.label} {c.active && <span className="badge downloaded">active</span>}
                  </td>
                  <td>{VPN_PROVIDERS.find((p) => p.id === c.provider)?.name ?? c.provider}</td>
                  <td>{c.endpoint}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button onClick={() => activate(c.id)} disabled={c.active}>
                      Activate
                    </button>{' '}
                    <button onClick={() => remove(c.id)}>Remove</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  )
}

export default function VPNSettings() {
  return (
    <div className="settings-stack">
      <VPNSection />
    </div>
  )
}
