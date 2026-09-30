// What the torrent port is doing, in plain words, for Settings > Downloading.
// Kept apart from the page so the wording can be tested.

export interface TorrentPortInfo {
  state?: string
  listenPort: number
  vpnState?: string
  blockedByVpn: boolean
  listening?: boolean
  activePort?: number
  incomingSeen?: boolean
}

export interface PortStatusText {
  tone: 'good' | 'info' | 'warn'
  title: string
  text: string
}

export function portStatus(t: TorrentPortInfo | null): PortStatusText | null {
  if (!t || t.state === 'disabled') return null
  if (t.vpnState === 'connected' || t.vpnState === 'connecting') {
    return {
      tone: 'info',
      title: 'Not used while your VPN is on.',
      text: "Torrents go through the VPN, so this port isn't used. Port forwarding through a VPN isn't supported.",
    }
  }
  if (t.blockedByVpn) {
    return {
      tone: 'warn',
      title: 'Waiting for your VPN.',
      text: 'Torrents are on hold until it connects, so this port is closed.',
    }
  }
  if (!t.listening) {
    return {
      tone: 'info',
      title: 'Closed for now.',
      text: 'It opens when a torrent is downloading or seeding.',
    }
  }
  const port = t.activePort || t.listenPort
  const moved = t.activePort && t.activePort !== t.listenPort ? ` Port ${t.listenPort} was already in use, so Mediarium is using ${t.activePort} instead.` : ''
  if (t.incomingSeen) {
    return {
      tone: moved ? 'warn' : 'good',
      title: `Listening on port ${port}.`,
      text: `Another peer has connected to you, so the port works from the internet.${moved}`,
    }
  }
  return {
    tone: moved ? 'warn' : 'info',
    title: `Listening on port ${port}.`,
    text: `No peer has connected yet, which is normal at first. If it stays that way, forward this port (TCP and UDP) on your router.${moved}`,
  }
}
