import { useEffect, useState } from 'react'
import { useToast } from '../../components/Toast'
import { api } from '../../api'

export default function TorrentSection() {
  const [listenPort, setListenPort] = useState('0')
  const [seedRatioLimit, setSeedRatioLimit] = useState('0')
  const [seedTimeLimitH, setSeedTimeLimitH] = useState('0')
  const toast = useToast()

  useEffect(() => {
    api.getSettings().then((s) => {
      setListenPort(s.torrentListenPort || '0')
      setSeedRatioLimit(s.torrentSeedRatioLimit || '0')
      setSeedTimeLimitH(s.torrentSeedTimeLimitH || '0')
    })
  }, [])

  async function save() {
        try {
      await api.putSettings({ torrentListenPort: listenPort, torrentSeedRatioLimit: seedRatioLimit, torrentSeedTimeLimitH: seedTimeLimitH })
      toast.success('Saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="card grid-form">
      <h2>Client settings</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        Torrent settings for the built-in client. There's no separate torrent program to set up. The defaults are fine for most people.
      </p>
      <label>
        Listen port (0 = pick automatically)
        <input value={listenPort} onChange={(e) => setListenPort(e.target.value)} />
      </label>
      <label>
        Seed ratio limit (0 = unlimited)
        <input value={seedRatioLimit} onChange={(e) => setSeedRatioLimit(e.target.value)} />
      </label>
      <label>
        Seed time limit, hours (0 = unlimited)
        <input value={seedTimeLimitH} onChange={(e) => setSeedTimeLimitH(e.target.value)} />
      </label>
      <button className="primary" onClick={save}>
        Save
      </button>
    </section>
  )
}
