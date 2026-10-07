import { useEffect, useState } from 'react'
import Switch from '../../components/Switch'
import { useAutosaveSetting } from '../../useAutosave'
import { api, type VPNConfig, type VPNStatus } from '../../api'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'
import { firstError, hostPort, ipWithPrefix, maxLength, required, wireguardKey } from '../../validate'
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { parseWireGuardConf } from '../../wgConf'

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
    note: 'Log in with your account number, generate a key, pick a server and download the "Linux" config. Then paste its fields below.',
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
    note: "PIA has no simple config page. Their official script (linked) makes a WireGuard config from your login, and you run it on your own machine. Then paste the fields below.",
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
    note: 'Get an access token first, then use it to get your WireGuard private key. Their page has the steps. Then paste the fields below.',
  },
  {
    id: 'windscribe',
    name: 'Windscribe',
    helpUrl: 'https://windscribe.com/getconfig/wireguard',
    note: 'Sign in, pick a location and a port, and download the config. It includes a preshared key, so paste the whole file below.',
  },
  {
    id: 'custom',
    name: 'Custom / other provider',
    helpUrl: '',
    note: 'Works for any provider or your own WireGuard server. Paste the config fields below.',
  },
] as const

function VPNSection() {
  const [list, setList] = useState<VPNConfig[]>([])
  const confirm = useConfirm()
  const [status, setStatus] = useState<VPNStatus | null>(null)
  const [busy, setBusy] = useState<number | 'off' | null>(null)
  const [providerId, setProviderId] = useState<string>(VPN_PROVIDERS[0].id)
  const [label, setLabel] = useState('')
  const [privateKey, setPrivateKey] = useState('')
  const [peerPublicKey, setPeerPublicKey] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [localAddress, setLocalAddress] = useState('')
  const [presharedKey, setPresharedKey] = useState('')
  const [dns, setDns] = useState('')
  const [allowedIps, setAllowedIps] = useState('')
  const [confText, setConfText] = useState('')
  const [confNote, setConfNote] = useState('')
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
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
  // A connection that is starting up is checked every few seconds, so the page
  // moves on from "Connecting" as soon as the VPN server answers.
  useLive(reload, status?.state === 'connecting' ? 3000 : 10000)

  const provider = VPN_PROVIDERS.find((p) => p.id === providerId) ?? VPN_PROVIDERS[0]

  // A pasted or chosen .conf file fills in every field below.
  function fillFromConf(text: string) {
    setConfText(text)
    if (text.trim() === '') {
      setConfNote('')
      return
    }
    const c = parseWireGuardConf(text)
    if (!c) {
      setConfNote("That doesn't look like a WireGuard config. It should have an [Interface] and a [Peer] part.")
      return
    }
    setPrivateKey(c.privateKey)
    setPeerPublicKey(c.peerPublicKey)
    setPresharedKey(c.presharedKey)
    setEndpoint(c.endpoint)
    setLocalAddress(c.localAddresses.join(', '))
    setDns(c.dns.join(', '))
    setAllowedIps(c.allowedIps.join(', '))
    setConfNote(c.presharedKey ? 'Filled in from your config, including the preshared key. Check the fields below and add a label.' : 'Filled in from your config. Check the fields below and add a label.')
  }

  function readConfFile(file: File | undefined) {
    if (!file) return
    if (file.size > 64 * 1024) {
      setConfNote('That file is too big to be a WireGuard config.')
      return
    }
    file.text().then(fillFromConf, () => setConfNote("Couldn't read that file."))
  }

  const split = (v: string) =>
    v
      .split(',')
      .map((x) => x.trim())
      .filter(Boolean)
  const eachOf = (v: string, check: (x: string) => string | null) => split(v).map(check).find((m) => m) ?? null

  const errors = {
    label: firstError(required(label, 'Give this connection a name, for example My VPN.'), maxLength(label, 60, 'The name')),
    endpoint: firstError(required(endpoint, 'Add your VPN server address and port, for example vpn.example.com:51820.'), hostPort(endpoint)),
    privateKey: firstError(required(privateKey, "Paste the private key from your provider's WireGuard config."), wireguardKey(privateKey, 'The private key')),
    peerPublicKey: firstError(required(peerPublicKey, "Paste the server's public key from your provider's WireGuard config."), wireguardKey(peerPublicKey, 'The public key')),
    localAddress: firstError(required(localAddress, 'Add the tunnel address from your config, for example 10.2.0.2/32.'), eachOf(localAddress, (x) => ipWithPrefix(x))),
    presharedKey: wireguardKey(presharedKey, 'The preshared key'),
    dns: eachOf(dns, (x) => (x.includes('/') ? 'Write DNS servers without a /, for example 10.2.0.1.' : ipWithPrefix(x, '10.2.0.1'))),
    allowedIps: eachOf(allowedIps, (x) => ipWithPrefix(x, '0.0.0.0/0')),
  }
  const v = useValidation(errors)

  async function add() {
    if (!v.attempt()) return
    setError('')
    setAdding(true)
    try {
      await api.createVPNConfig({
        label,
        provider: providerId,
        privateKey,
        peerPublicKey,
        presharedKey: presharedKey.trim() || undefined,
        endpoint,
        localAddresses: split(localAddress),
        dns: split(dns),
        allowedIps: split(allowedIps),
      })
      setLabel('')
      setPrivateKey('')
      setPeerPublicKey('')
      setPresharedKey('')
      setEndpoint('')
      setLocalAddress('')
      setDns('')
      setAllowedIps('')
      setConfText('')
      setConfNote('')
      v.reset()
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setAdding(false)
    }
  }

  async function activate(id: number) {
    setError('')
    setBusy(id)
    try {
      await api.activateVPNConfig(id)
      setEgressIp('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
      reload()
    }
  }

  async function deactivate() {
    setError('')
    setBusy('off')
    try {
      await api.deactivateVPN()
      setEgressIp('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
      reload()
    }
  }

  async function remove(c: VPNConfig) {
    if (!(await confirm({ title: `Remove "${c.label}"?`, body: <p>{c.active ? 'It is switched on, so the VPN disconnects. ' : ''}You can add it again later by pasting its details.</p>, confirmLabel: 'Remove', danger: true }))) return
    setError('')
    try {
      await api.deleteVPNConfig(c.id)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      reload()
    }
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
          Built-in WireGuard tunnel. It carries only torrent traffic and needs no extra container or privileges.
        </p>
        {error && <p className="error-text">{error}</p>}
        {status && (
          <p>
            Status:{' '}
            <span className={`badge ${status.connected ? 'downloaded' : status.state === 'connecting' ? 'downloading' : status.state === 'down' ? 'failed' : 'missing'}`}>
              {status.connected
                ? `Connected (${status.label})`
                : status.state === 'connecting'
                  ? `Connecting (${status.label})…`
                  : status.state === 'down'
                    ? `Disconnected (${status.label})`
                    : 'Disconnected'}
            </span>
            {status.label && (
              <button style={{ marginLeft: 8 }} onClick={deactivate} disabled={busy === 'off'}>
                {busy === 'off' ? 'Disconnecting…' : 'Disconnect'}
              </button>
            )}
          </p>
        )}
        {status?.label && !status.connected && (
          <div className="notice notice-warn">
            <strong>{status.state === 'connecting' ? 'Connecting to your VPN.' : `${status.label} isn't connected.`}</strong>{' '}
            {status.reason} Torrents wait until it connects, so nothing goes out without the VPN. Press Disconnect to download without it.
          </div>
        )}
        {status?.connected && (
          <p style={{ fontSize: '0.9rem' }}>
            Public IP address: {egressIp || '—'} <button onClick={checkEgressIp}>Check my IP address</button>{' '}
            {egressIpStatus && <span style={{ color: 'var(--text-dim)' }}>{egressIpStatus}</span>}
          </p>
        )}
        {status && !status.label && (
          <div className="notice notice-warn">
            <strong>No VPN connected.</strong> Everyone in a torrent swarm can see your home IP address, and your internet
            provider can see you're torrenting. A VPN hides both. Usenet downloads are encrypted with SSL and don't expose you this way.
          </div>
        )}
        <Switch
          checked={kill.value}
          onChange={kill.change}
          disabled={!kill.loaded || kill.saving}
          label="Kill switch: only run torrents while a VPN is connected"
          description="Torrents wait instead of connecting without the tunnel. DHT and uTP stay off through the tunnel."
          showState
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
          Every provider uses the same WireGuard fields below.
        </p>
        <label className="span-all">
          Paste your .conf file (optional)
          <textarea
            value={confText}
            onChange={(e) => fillFromConf(e.target.value)}
            rows={5}
            spellCheck={false}
            autoComplete="off"
            placeholder={'[Interface]\nPrivateKey = ...\nAddress = 10.2.0.2/32\n\n[Peer]\nPublicKey = ...\nEndpoint = vpn.example.com:51820'}
            style={{ fontFamily: 'var(--font-mono)', fontSize: '0.82rem' }}
          />
        </label>
        <div className="span-all" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12 }}>
          <label className="btn-with-icon" style={{ display: 'inline-flex', cursor: 'pointer' }}>
            <input type="file" accept=".conf,text/plain" onChange={(e) => readConfFile(e.target.files?.[0])} style={{ position: 'absolute', width: 1, height: 1, opacity: 0 }} />
            Or choose the file
          </label>
          {confNote && <small style={{ color: 'var(--text-dim)' }}>{confNote}</small>}
        </div>
        <label>
          Label
          <input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="My VPN" {...v.bind('label', label, setLabel)} />
          <FieldError v={v} name="label" />
        </label>
        <label>
          Endpoint
          <input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="vpn.example.com:51820" {...v.bind('endpoint', endpoint, setEndpoint)} />
          <FieldError v={v} name="endpoint" />
        </label>
        <label>
          Private key
          <input type="password" value={privateKey} onChange={(e) => setPrivateKey(e.target.value)} placeholder="base64 private key" autoComplete="off" spellCheck={false} {...v.bind('privateKey', privateKey, setPrivateKey)} />
          <FieldError v={v} name="privateKey" />
        </label>
        <label>
          Peer public key
          <input value={peerPublicKey} onChange={(e) => setPeerPublicKey(e.target.value)} placeholder="base64 public key" spellCheck={false} {...v.bind('peerPublicKey', peerPublicKey, setPeerPublicKey)} />
          <FieldError v={v} name="peerPublicKey" />
        </label>
        <label>
          Preshared key (optional)
          <input type="password" value={presharedKey} onChange={(e) => setPresharedKey(e.target.value)} placeholder="only if your config has one" autoComplete="off" spellCheck={false} {...v.bind('presharedKey', presharedKey, setPresharedKey)} />
          <FieldError v={v} name="presharedKey" />
        </label>
        <label>
          Local tunnel address
          <input value={localAddress} onChange={(e) => setLocalAddress(e.target.value)} placeholder="10.2.0.2/32" {...v.bind('localAddress', localAddress, setLocalAddress)} />
          <FieldError v={v} name="localAddress" />
        </label>
        <label>
          DNS servers (optional)
          <input value={dns} onChange={(e) => setDns(e.target.value)} placeholder="10.2.0.1" spellCheck={false} {...v.bind('dns', dns, setDns)} />
          <FieldError v={v} name="dns" />
        </label>
        <label>
          Allowed IPs (optional)
          <input value={allowedIps} onChange={(e) => setAllowedIps(e.target.value)} placeholder="0.0.0.0/0" spellCheck={false} {...v.bind('allowedIps', allowedIps, setAllowedIps)} />
          <FieldError v={v} name="allowedIps" />
        </label>
        <div>
          <button className="primary" onClick={add} disabled={adding}>
            {adding ? 'Adding…' : 'Add connection'}
          </button>
          <FormProblem v={v} verb="add this connection" />
        </div>
      </section>

      <section className="card">
        <h2>Your VPN connections</h2>
        {list.length === 0 ? (
          <p style={{ color: 'var(--text-dim)' }}>None yet. Add one to protect torrent traffic.</p>
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
                    {c.label}{' '}
                    {c.active && (
                      <span className={`badge ${status?.connected ? 'downloaded' : status?.state === 'connecting' ? 'downloading' : 'failed'}`}>
                        {status?.connected ? 'connected' : status?.state === 'connecting' ? 'connecting' : 'not connected'}
                      </span>
                    )}
                  </td>
                  <td>{VPN_PROVIDERS.find((p) => p.id === c.provider)?.name ?? c.provider}</td>
                  <td>{c.endpoint}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button onClick={() => activate(c.id)} disabled={busy !== null || (c.active && !!status?.connected)}>
                      {busy === c.id ? 'Connecting…' : c.active && !status?.connected ? 'Reconnect' : 'Activate'}
                    </button>{' '}
                    <button onClick={() => void remove(c)}>Remove</button>
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
