// Reads a WireGuard .conf file (the one a VPN provider lets you download) into
// the fields the VPN form asks for, so people can paste the whole file instead
// of copying each value by hand.
export interface WireGuardConf {
  privateKey: string
  localAddresses: string[]
  dns: string[]
  peerPublicKey: string
  presharedKey: string
  endpoint: string
  allowedIps: string[]
}

const list = (v: string) =>
  v
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

// Returns null when the text has no [Interface] or [Peer] section at all.
export function parseWireGuardConf(text: string): WireGuardConf | null {
  const out: WireGuardConf = { privateKey: '', localAddresses: [], dns: [], peerPublicKey: '', presharedKey: '', endpoint: '', allowedIps: [] }
  let section = ''
  let sawSection = false
  let peers = 0
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.replace(/\s[#;].*$/, '').trim()
    if (line === '' || line.startsWith('#') || line.startsWith(';')) continue
    const head = /^\[(\w+)\]$/.exec(line)
    if (head) {
      section = head[1].toLowerCase()
      sawSection = sawSection || section === 'interface' || section === 'peer'
      // A second [Peer] is left out: Mediarium uses the first one only.
      if (section === 'peer' && ++peers > 1) section = 'ignored'
      continue
    }
    const eq = line.indexOf('=')
    if (eq < 0) continue
    const key = line.slice(0, eq).trim().toLowerCase()
    const value = line.slice(eq + 1).trim()
    if (section === 'interface') {
      if (key === 'privatekey') out.privateKey = value
      else if (key === 'address') out.localAddresses.push(...list(value))
      // DNS may also name search domains; only addresses are kept.
      else if (key === 'dns') out.dns.push(...list(value).filter((d) => /^[0-9.]+$|:/.test(d)))
    } else if (section === 'peer') {
      if (key === 'publickey') out.peerPublicKey = value
      else if (key === 'presharedkey') out.presharedKey = value
      else if (key === 'endpoint') out.endpoint = value
      else if (key === 'allowedips') out.allowedIps.push(...list(value))
    }
  }
  return sawSection ? out : null
}
