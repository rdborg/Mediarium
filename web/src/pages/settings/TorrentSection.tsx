import { useEffect, useState } from 'react'
import { useToast } from '../../components/Toast'
import { api, type DownloadsStatus } from '../../api'
import { portStatus } from './torrentPort'
import { firstError, numberRange } from '../../validate'
import { FieldError, FormProblem, useValidation } from '../../useValidation'

// What the torrent port is doing right now, in plain words. The wording comes
// from portStatus so it can be tested.
function PortStatus({ status }: { status: DownloadsStatus['torrent'] | null }) {
  const info = portStatus(status)
  if (!info) return null
  return (
    <div className={`notice ${info.tone === 'warn' ? 'notice-warn' : 'notice-info'}`} role="status">
      <strong>{info.title}</strong> {info.text}
    </div>
  )
}

export default function TorrentSection({ status }: { status: DownloadsStatus['torrent'] | null }) {
  const [listenPort, setListenPort] = useState('58264')
  const [seedRatioLimit, setSeedRatioLimit] = useState('0')
  const [seedTimeLimitH, setSeedTimeLimitH] = useState('0')
  const toast = useToast()
  // Saving before the current values have arrived would overwrite them with the defaults.
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const errors = {
    listenPort: firstError(
      listenPort.trim() === '' && 'Enter a port number, or 0 for the default (58264).',
      numberRange(listenPort, 'The listen port', { min: 0, max: 65535 }),
    ),
    seedRatioLimit: firstError(
      seedRatioLimit.trim() === '' && 'Enter a seed ratio, or 0 for no limit.',
      numberRange(seedRatioLimit, 'The seed ratio limit', { min: 0, max: 1000, decimal: true }),
    ),
    seedTimeLimitH: firstError(
      seedTimeLimitH.trim() === '' && 'Enter a number of hours, or 0 for no limit.',
      numberRange(seedTimeLimitH, 'The seed time limit', { min: 0, max: 87600, decimal: true }),
    ),
  }
  const v = useValidation(errors)

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setListenPort(s.torrentListenPort || '58264')
        setSeedRatioLimit(s.torrentSeedRatioLimit || '0')
        setSeedTimeLimitH(s.torrentSeedTimeLimitH || '0')
        setLoaded(true)
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [])

  async function save() {
    if (!v.attempt()) return
    setSaving(true)
    try {
      await api.putSettings({ torrentListenPort: listenPort, torrentSeedRatioLimit: seedRatioLimit, torrentSeedTimeLimitH: seedTimeLimitH })
      toast.success('Saved.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="card grid-form">
      <h2>Client settings</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        The defaults are fine for most people.
      </p>
      <label>
        Listen port (0 = the default, 58264)
        <input inputMode="numeric" value={listenPort} onChange={(e) => setListenPort(e.target.value)} {...v.bind('listenPort', listenPort, setListenPort)} />
        <FieldError v={v} name="listenPort" />
      </label>
      <PortStatus status={status} />
      <label>
        Seed ratio limit (0 = unlimited)
        <input inputMode="decimal" value={seedRatioLimit} onChange={(e) => setSeedRatioLimit(e.target.value)} {...v.bind('seedRatioLimit', seedRatioLimit, setSeedRatioLimit)} />
        <FieldError v={v} name="seedRatioLimit" />
      </label>
      <label>
        Seed time limit, hours (0 = unlimited)
        <input inputMode="decimal" value={seedTimeLimitH} onChange={(e) => setSeedTimeLimitH(e.target.value)} {...v.bind('seedTimeLimitH', seedTimeLimitH, setSeedTimeLimitH)} />
        <FieldError v={v} name="seedTimeLimitH" />
      </label>
      {loadError && <p className="error-text">Could not load the current values: {loadError}</p>}
      <button className="primary" onClick={save} disabled={!loaded || saving}>
        Save
      </button>
      <FormProblem v={v} />
    </section>
  )
}
