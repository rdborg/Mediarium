import { useEffect, useState } from 'react'
import Switch from '../../components/Switch'
import ProxyCard from '../../components/ProxyCard'
import { useAutosaveSetting } from '../../useAutosave'
import { api, type VPNConfig, type VPNStatus, type VPNTestResult } from '../../api'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'
import { firstError, hostPort, ipWithPrefix, maxLength, required, wireguardKey } from '../../validate'
import { FieldError, FormProblem, useValidation } from '../../useValidation'
import { parseWireGuardConf, type WireGuardConf } from '../../wgConf'
import { OTHER_PROVIDER, confHints, providerName, vpnProviders } from '../../vpnProviders'
import { t, st } from '../../i18n'
import TipLabel from '../../components/TipLabel'
import { flareSolverrSetUp } from './vpnIndexers'
import { handshakeLine, testSummary, trafficLine } from '../../vpnHealth'

// What is known about the connection that is switched on: when its server last
// answered, how much it carried, and the outcome of the last Test.
function ConnectionHealth({ status, result, error }: { status: VPNStatus | null; result: VPNTestResult | null; error: string }) {
  const handshake = handshakeLine(status)
  const traffic = trafficLine(status)
  return (
    <>
      {handshake && (
        <div className={handshake.warn ? 'error-text' : undefined} style={{ fontSize: '0.85rem', color: handshake.warn ? undefined : 'var(--text-dim)' }}>
          {handshake.text}
        </div>
      )}
      {traffic && <div style={{ fontSize: '0.85rem', color: 'var(--text-dim)' }}>{traffic}</div>}
      {result && (
        <div className={`notice ${result.ok ? 'notice-info' : 'notice-danger'}`} role="status" style={{ margin: '6px 0 0' }}>
          {testSummary(result)}
        </div>
      )}
      {error && <div className="error-text" style={{ fontSize: '0.85rem' }}>{error}</div>}
    </>
  )
}

function VPNSection() {
  const [list, setList] = useState<VPNConfig[]>([])
  const confirm = useConfirm()
  const [status, setStatus] = useState<VPNStatus | null>(null)
  const [busy, setBusy] = useState<number | 'off' | null>(null)
  // Nothing is chosen at first: the page doesn't lean towards any provider.
  const [providerId, setProviderId] = useState('')
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
  const [conf, setConf] = useState<WireGuardConf | null>(null)
  const [error, setError] = useState('')
  const [added, setAdded] = useState('')
  const [adding, setAdding] = useState(false)
  const kill = useAutosaveSetting<boolean>(
    (s) => !!s.requireVpnForTorrents,
    (v) => ({ requireVpnForTorrents: v }),
    (saved) => (saved ? t('Saved: torrents now only run while a VPN is connected.') : t('Saved: torrents no longer require a VPN.')),
    false,
  )
  const indexers = useAutosaveSetting<boolean>(
    (s) => !!s.routeIndexersThroughVpn,
    (v) => ({ routeIndexersThroughVpn: v }),
    (saved) => (saved ? t('Saved: indexer traffic now goes through the VPN.') : t('Saved: indexer traffic now connects directly.')),
    false,
  )
  const discover = useAutosaveSetting<boolean>(
    (s) => !!s.discoverThroughTunnel,
    (v) => ({ discoverThroughTunnel: v }),
    (saved) => (saved ? t('Saved: UDP trackers and DHT now go through the VPN.') : t('Saved: UDP trackers and DHT stay off while the VPN is on.')),
    false,
  )
  const acknowledged = useAutosaveSetting<boolean>(
    (s) => !!s.vpnNoticeAcknowledged,
    (v) => ({ vpnNoticeAcknowledged: v }),
    (saved) => (saved ? t('Saved: the notice about torrenting without a VPN is hidden.') : t('Saved: the notice about torrenting without a VPN can show again.')),
    false,
  )
  const [flareSolverr, setFlareSolverr] = useState(false)
  useEffect(() => {
    api.getSettings().then((s) => setFlareSolverr(flareSolverrSetUp(s))).catch(() => undefined)
  }, [])
  const [egressIp, setEgressIp] = useState('')
  const [egressIpStatus, setEgressIpStatus] = useState('')
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<VPNTestResult | null>(null)
  const [testError, setTestError] = useState('')

  function reload() {
    api.listVPNConfigs().then(setList).catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.vpnStatus().then(setStatus).catch(() => {})
  }
  useEffect(reload, [])
  // A connection that is starting up is checked every few seconds, so the page
  // moves on from "Connecting" as soon as the VPN server answers.
  useLive(reload, status?.state === 'connecting' ? 3000 : 10000)

  const providers = vpnProviders()
  const provider = providers.find((p) => p.id === providerId) ?? null
  const hints = provider && conf ? confHints(provider, conf) : []

  // Picking a provider fills in what is the same for all its customers (its DNS
  // servers). Whatever was typed or pasted stays, and a value that came from the
  // previous provider is swapped for the new one.
  function chooseProvider(id: string) {
    const next = providers.find((p) => p.id === id) ?? null
    const before = provider?.dns.join(', ') ?? ''
    if (dns.trim() === '' || dns === before) setDns(next?.dns.join(', ') ?? '')
    setProviderId(id)
  }

  // A pasted or chosen .conf file fills in every field below.
  function fillFromConf(text: string) {
    setConfText(text)
    setAdded('')
    if (text.trim() === '') {
      setConfNote('')
      setConf(null)
      return
    }
    const c = parseWireGuardConf(text)
    setConf(c)
    if (!c) {
      setConfNote(t("That doesn't look like a WireGuard config. It should have an [Interface] and a [Peer] part."))
      return
    }
    setPrivateKey(c.privateKey)
    setPeerPublicKey(c.peerPublicKey)
    setPresharedKey(c.presharedKey)
    setEndpoint(c.endpoint)
    setLocalAddress(c.localAddresses.join(', '))
    // A file without a DNS line gets the provider's, when it has one.
    setDns(c.dns.length > 0 ? c.dns.join(', ') : (provider?.dns.join(', ') ?? ''))
    setAllowedIps(c.allowedIps.join(', '))
    setConfNote(c.presharedKey ? t('Filled in from your config, including the preshared key. Check the fields below and add a label.') : t('Filled in from your config. Check the fields below and add a label.'))
  }

  function readConfFile(file: File | undefined) {
    if (!file) return
    if (file.size > 64 * 1024) {
      setConfNote(t('That file is too big to be a WireGuard config.'))
      return
    }
    file.text().then(fillFromConf, () => setConfNote(t("Couldn't read that file.")))
  }

  const split = (v: string) =>
    v
      .split(',')
      .map((x) => x.trim())
      .filter(Boolean)
  const eachOf = (v: string, check: (x: string) => string | null) => split(v).map(check).find((m) => m) ?? null

  const errors = {
    label: firstError(required(label, t('Give this connection a name, for example My VPN.')), maxLength(label, 60, t('The name'))),
    endpoint: firstError(required(endpoint, t('Add your VPN server address and port, for example vpn.example.com:51820.')), hostPort(endpoint)),
    privateKey: firstError(required(privateKey, t("Paste the private key from your provider's WireGuard config.")), wireguardKey(privateKey, t('The private key'))),
    peerPublicKey: firstError(required(peerPublicKey, t("Paste the server's public key from your provider's WireGuard config.")), wireguardKey(peerPublicKey, t('The public key'))),
    localAddress: firstError(required(localAddress, t('Add the tunnel address from your config, for example 10.2.0.2/32.')), eachOf(localAddress, (x) => ipWithPrefix(x))),
    presharedKey: wireguardKey(presharedKey, t('The preshared key')),
    dns: eachOf(dns, (x) => (x.includes('/') ? t('Write DNS servers without a /, for example 10.2.0.1.') : ipWithPrefix(x, '10.2.0.1'))),
    allowedIps: eachOf(allowedIps, (x) => ipWithPrefix(x, '0.0.0.0/0')),
  }
  const v = useValidation(errors)

  async function add() {
    if (!v.attempt()) return
    setError('')
    setAdded('')
    setAdding(true)
    try {
      await api.createVPNConfig({
        label,
        provider: providerId || OTHER_PROVIDER,
        privateKey,
        peerPublicKey,
        presharedKey: presharedKey.trim() || undefined,
        endpoint,
        localAddresses: split(localAddress),
        dns: split(dns),
        allowedIps: split(allowedIps),
      })
      setAdded(t('Added "{name}". Press Activate in your list below to connect.', { name: label }))
      setLabel('')
      setPrivateKey('')
      setPeerPublicKey('')
      setPresharedKey('')
      setEndpoint('')
      setLocalAddress('')
      setDns(provider?.dns.join(', ') ?? '')
      setAllowedIps('')
      setConfText('')
      setConfNote('')
      setConf(null)
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
    if (
      !(await confirm({
        title: t('Remove "{name}"?', { name: c.label }),
        body: <p>{c.active ? t('It is switched on, so the VPN disconnects. You can add it again later by pasting its details.') : t('You can add it again later by pasting its details.')}</p>,
        confirmLabel: t('Remove'),
        danger: true,
      }))
    )
      return
    setError('')
    try {
      await api.deleteVPNConfig(c.id)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      reload()
    }
  }

  // A small real request through the tunnel: shows whether traffic really flows,
  // not just whether the connection looks up.
  async function runTest() {
    setTesting(true)
    setTestResult(null)
    setTestError('')
    try {
      setTestResult(await api.vpnTest())
    } catch (e) {
      setTestError(e instanceof Error ? e.message : String(e))
    } finally {
      setTesting(false)
      reload()
    }
  }

  async function checkEgressIp() {
    setEgressIpStatus(t('Checking…'))
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
        <h2>{t('VPN status')}</h2>
        <p style={{ color: 'var(--text-dim)' }}>
          {t('Built-in WireGuard tunnel. It carries only torrent traffic and needs no extra container or privileges.')}
        </p>
        {error && <p className="error-text">{error}</p>}
        {status && (
          <p>
            {t('Status:')}{' '}
            <span className={`badge ${status.connected ? 'downloaded' : status.state === 'connecting' ? 'downloading' : status.state === 'down' ? 'failed' : 'missing'}`}>
              {status.connected
                ? t('Connected ({name})', { name: status.label ?? '' })
                : status.state === 'connecting'
                  ? t('Connecting ({name})…', { name: status.label ?? '' })
                  : status.state === 'down'
                    ? t('Disconnected ({name})', { name: status.label ?? '' })
                    : t('Disconnected')}
            </span>
            {status.label && (
              <button style={{ marginLeft: 8 }} onClick={deactivate} disabled={busy === 'off'}>
                {busy === 'off' ? t('Disconnecting…') : t('Disconnect')}
              </button>
            )}
          </p>
        )}
        {status?.label && !status.connected && (
          <div className="notice notice-warn">
            <strong>{status.state === 'connecting' ? t('Connecting to your VPN.') : t("{name} isn't connected.", { name: status.label ?? '' })}</strong>{' '}
            {st(status.reason)} {t('Torrents wait until it connects, so nothing goes out without the VPN. Press Disconnect to download without it.')}
          </div>
        )}
        {status?.connected && (
          <p style={{ fontSize: '0.9rem' }}>
            {t('Public IP address: {ip}', { ip: egressIp || '—' })} <button onClick={checkEgressIp}>{t('Check my IP address')}</button>{' '}
            {egressIpStatus && <span style={{ color: 'var(--text-dim)' }}>{egressIpStatus}</span>}
          </p>
        )}
        {status && !status.label && (
          <div className="notice notice-warn">
            <strong>{t('No VPN connected.')}</strong>{' '}
            {t("Everyone in a torrent swarm can see your home IP address, and your internet provider can see you're torrenting. A VPN hides both. Usenet downloads are encrypted with SSL and don't expose you this way.")}
          </div>
        )}
        <Switch
          checked={kill.value}
          onChange={kill.change}
          disabled={!kill.loaded || kill.saving}
          label={t('Kill switch: only run torrents while a VPN is connected')}
          tip={t('When no VPN is connected, torrents wait instead of connecting directly, so the swarm never sees your real address. A connected VPN is always used.')}
          learnMore="downloads#the-kill-switch"
          description={t('Torrents wait instead of connecting without the tunnel. A proxy for torrents (below) counts too. uTP stays off through the tunnel.')}
          showState
        />
        <Switch
          checked={discover.value}
          onChange={discover.change}
          disabled={!discover.loaded || discover.saving}
          label={t('UDP trackers and DHT through the VPN')}
          tip={t('Peers and trackers see the VPN address. Tracker and DHT names are looked up through the tunnel, and if that fails nothing is sent from your home address.')}
          learnMore="downloads#udp-trackers-and-dht"
          description={t("Announces to UDP trackers and DHT queries go out through the tunnel, so peers and trackers see the VPN address. Tracker and DHT names are looked up through the VPN's DNS, not from your home connection, and a failed lookup does not go out. While this is off, those are skipped and only HTTP trackers are used. A download that has already started keeps the previous choice until you retry it.")}
          showState
        />
        <Switch
          checked={indexers.value}
          onChange={indexers.change}
          disabled={!indexers.loaded || indexers.saving}
          label={t('Send indexer traffic through the VPN')}
          tip={t("Your indexers see the VPN's address instead of yours, and their names are looked up through the tunnel too.")}
          learnMore="downloads#indexers-through-the-vpn"
          description={t('Searches, RSS, release downloads, IRC announces and debrid go through the tunnel. Indexers on your own network still connect directly. If the VPN is down, searches wait instead of going without it.')}
          showState
        />
        <Switch
          checked={acknowledged.value}
          onChange={acknowledged.change}
          disabled={!acknowledged.loaded || acknowledged.saving}
          label={t('My download client is already behind a VPN')}
          description={t('Hides the "Torrenting without a VPN" notice on the dashboard. Switch it off to show the notice again.')}
          showState
        />
        {indexers.value && flareSolverr && (
          <div className="notice notice-warn">
            {t("Sites behind a Cloudflare check are fetched by FlareSolverr from its own network, so they see that address (your server address for the built-in one), not the VPN's. Some may also stop working.")}
          </div>
        )}
      </section>

      <section className="card">
        <h2>{t('Your VPN connections')}</h2>
        {list.length === 0 ? (
          <p style={{ color: 'var(--text-dim)' }}>{t('None yet. Add one to protect torrent traffic.')}</p>
        ) : (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 12 }}>
            {list.map((c) => (
              <li key={c.id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                <div style={{ minWidth: 0, overflowWrap: 'anywhere' }}>
                  <strong>{c.label}</strong>{' '}
                  {c.active && (
                    <span className={`badge ${status?.connected ? 'downloaded' : status?.state === 'connecting' ? 'downloading' : 'failed'}`}>
                      {status?.connected ? t('connected') : status?.state === 'connecting' ? t('connecting') : t('not connected')}
                    </span>
                  )}
                  <div style={{ color: 'var(--text-dim)', fontSize: '0.85rem' }}>
                    {providerName(c.provider)} · {c.endpoint}
                  </div>
                  {c.secretError && <div className="error-text" style={{ fontSize: '0.85rem' }}>{st(c.secretError)}</div>}
                  {c.active && <ConnectionHealth status={status} result={testResult} error={testError} />}
                </div>
                <div style={{ whiteSpace: 'nowrap' }}>
                  <button onClick={() => activate(c.id)} disabled={busy !== null || !!c.secretError || (c.active && !!status?.connected)}>
                    {busy === c.id ? t('Connecting…') : c.active && !status?.connected ? t('Reconnect') : t('Activate')}
                  </button>{' '}
                  {c.active && (
                    <>
                      <button onClick={() => void runTest()} disabled={testing || busy !== null}>
                        {testing ? t('Testing…') : t('Test')}
                      </button>{' '}
                    </>
                  )}
                  <button onClick={() => void remove(c)}>{t('Remove')}</button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="card grid-form span-all">
        <h2><TipLabel help="downloads#add-a-connection">{t('Add a VPN connection')}</TipLabel></h2>
        <label>
          <TipLabel text={t('Pick yours for step-by-step help getting its WireGuard file. If your provider is not listed, choose Not listed and paste the file.')} learnMore="downloads#providers-and-where-to-get-the-file">{t('Provider')}</TipLabel>
          <select value={providerId} onChange={(e) => chooseProvider(e.target.value)}>
            <option value="">{t('Choose your provider…')}</option>
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <div className="span-all" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 380px), 1fr))', gap: '14px 28px', alignItems: 'start' }}>
          <div aria-live="polite">
            {!provider && (
              <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: 0 }}>
                {t('Pick your provider for step-by-step help getting its WireGuard file. If you already have a file, you can skip this and paste it below.')}
              </p>
            )}
            {provider && (
              <>
                <p style={{ margin: '0 0 6px', fontWeight: 600 }}>{t('Get your WireGuard file from {name}', { name: provider.name })}</p>
                <ol className="steps">
                  {provider.steps.map((s) => (
                    <li key={s}>{s}</li>
                  ))}
                </ol>
                {provider.link && (
                  <p style={{ margin: '8px 0 0', fontSize: '0.9rem' }}>
                    <a href={provider.link} target="_blank" rel="noreferrer">
                      {provider.linkIsGuide ? t("Open {name}'s instructions ↗", { name: provider.name }) : t("Open {name}'s config page ↗", { name: provider.name })}
                    </a>
                  </p>
                )}
                {provider.notes.map((n) => (
                  <div key={n} className="notice notice-warn">
                    {n}
                  </div>
                ))}
              </>
            )}
          </div>
          <div style={{ display: 'grid', gap: 12 }}>
            <label>
              <TipLabel text={t('The WireGuard file from your VPN provider. Pasting it fills in every field below. Only the first [Peer] part is used.')} learnMore="downloads#add-a-connection">{t('Paste your .conf file (optional)')}</TipLabel>
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
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12 }}>
              <label className="btn-with-icon" style={{ display: 'inline-flex', cursor: 'pointer' }}>
                <input type="file" accept=".conf,text/plain" onChange={(e) => readConfFile(e.target.files?.[0])} style={{ position: 'absolute', width: 1, height: 1, opacity: 0 }} />
                {t('Or choose the file')}
              </label>
              {confNote && <small style={{ color: 'var(--text-dim)' }}>{confNote}</small>}
            </div>
            {hints.map((h) => (
              <div key={h} className="notice notice-warn" style={{ margin: 0 }}>
                {h}
              </div>
            ))}
          </div>
        </div>
        <p className="span-all" style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: 0 }}>
          {t('Check these details, or fill them in by hand if you have no file.')}
        </p>
        <label>
          {t('Label')}
          <input value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t('My VPN')} {...v.bind('label', label, setLabel)} />
          <FieldError v={v} name="label" />
        </label>
        <label>
          <TipLabel text={t('Your VPN server\'s address and port, like vpn.example.com:51820. It is the Endpoint line of the WireGuard file.')}>{t('Endpoint')}</TipLabel>
          <input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="vpn.example.com:51820" {...v.bind('endpoint', endpoint, setEndpoint)} />
          <FieldError v={v} name="endpoint" />
        </label>
        <label>
          <TipLabel text={t('Your secret WireGuard key from the file (44 characters). It is stored encrypted and never shown again.')}>{t('Private key')}</TipLabel>
          <input type="password" value={privateKey} onChange={(e) => setPrivateKey(e.target.value)} placeholder={t('base64 private key')} autoComplete="off" spellCheck={false} {...v.bind('privateKey', privateKey, setPrivateKey)} />
          <FieldError v={v} name="privateKey" />
        </label>
        <label>
          <TipLabel text={t('The VPN server\'s public key, from the PublicKey line in the [Peer] part of the file (44 characters).')}>{t('Peer public key')}</TipLabel>
          <input value={peerPublicKey} onChange={(e) => setPeerPublicKey(e.target.value)} placeholder={t('base64 public key')} spellCheck={false} {...v.bind('peerPublicKey', peerPublicKey, setPeerPublicKey)} />
          <FieldError v={v} name="peerPublicKey" />
        </label>
        <label>
          <TipLabel text={t('An extra shared secret that some providers add. If your file has a PresharedKey line, fill it in or the tunnel will not connect.')}>{t('Preshared key (optional)')}</TipLabel>
          <input type="password" value={presharedKey} onChange={(e) => setPresharedKey(e.target.value)} placeholder={t('only if your config has one')} autoComplete="off" spellCheck={false} {...v.bind('presharedKey', presharedKey, setPresharedKey)} />
          <FieldError v={v} name="presharedKey" />
        </label>
        <label>
          <TipLabel text={t('The address your provider gives your side of the tunnel, like 10.2.0.2/32. It is the Address line in the [Interface] part.')}>{t('Local tunnel address')}</TipLabel>
          <input value={localAddress} onChange={(e) => setLocalAddress(e.target.value)} placeholder="10.2.0.2/32" {...v.bind('localAddress', localAddress, setLocalAddress)} />
          <FieldError v={v} name="localAddress" />
        </label>
        <label>
          <TipLabel text={t('The name servers from the DNS line of your file, if it has one. Leave it empty if it has none.')}>{t('DNS servers (optional)')}</TipLabel>
          <input value={dns} onChange={(e) => setDns(e.target.value)} placeholder="10.2.0.1" spellCheck={false} {...v.bind('dns', dns, setDns)} />
          <FieldError v={v} name="dns" />
        </label>
        <label>
          <TipLabel text={t('Which traffic goes into the tunnel. Empty sends everything through it, which is what you want for torrents.')}>{t('Allowed IPs (optional)')}</TipLabel>
          <input value={allowedIps} onChange={(e) => setAllowedIps(e.target.value)} placeholder="0.0.0.0/0" spellCheck={false} {...v.bind('allowedIps', allowedIps, setAllowedIps)} />
          <FieldError v={v} name="allowedIps" />
        </label>
        <div>
          <button className="primary" onClick={add} disabled={adding}>
            {adding ? t('Adding…') : t('Add connection')}
          </button>
          <FormProblem v={v} action="connection" />
          {added && <p style={{ color: 'var(--text-dim)', fontSize: '0.9rem', margin: '8px 0 0' }}>{added}</p>}
        </div>
      </section>

    </>
  )
}

export default function VPNSettings() {
  return (
    <div className="settings-stack">
      <VPNSection />
      <ProxyCard />
    </div>
  )
}
