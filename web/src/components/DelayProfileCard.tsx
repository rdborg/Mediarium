import { useEffect, useState } from 'react'
import { api, type DelayProfile } from '../api'
import { plural, t, tx } from '../i18n'
import { firstError, numberRange } from '../validate'
import { useToast } from './Toast'
import Switch from './Switch'
import TipLabel from './TipLabel'

const MAX_MINUTES = 30 * 24 * 60

// "90 minutes" or "2 hours", for the line under the boxes.
function waitText(minutes: number): string {
  if (minutes <= 0) return t('no wait')
  if (minutes % 1440 === 0) return plural(minutes / 1440, '1 day', '{n} days')
  if (minutes % 60 === 0) return plural(minutes / 60, '1 hour', '{n} hours')
  return plural(minutes, '1 minute', '{n} minutes')
}

// Settings > Finding releases > Automatic searching: how long Mediarium's own
// searches wait for the protocol you like better before taking a release from
// the other one. Searches you start yourself never wait.
export default function DelayProfileCard() {
  const toast = useToast()
  const [saved, setSaved] = useState<DelayProfile | null>(null)
  const [preferred, setPreferred] = useState<'usenet' | 'torrent'>('usenet')
  const [usenet, setUsenet] = useState('0')
  const [torrent, setTorrent] = useState('0')
  const [bypass, setBypass] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .delayProfile()
      .then((p) => {
        setSaved(p)
        setPreferred(p.preferredProtocol)
        setUsenet(String(p.usenetMinutes))
        setTorrent(String(p.torrentMinutes))
        setBypass(p.bypassIfHighestQuality)
      })
      .catch(() => undefined)
  }, [])

  const range = (v: string, label: string) => firstError(v.trim() === '' && t('Enter a number from 0 to {max}.', { max: MAX_MINUTES }), numberRange(v, label, { min: 0, max: MAX_MINUTES }))
  const usenetError = range(usenet, t('The Usenet wait'))
  const torrentError = range(torrent, t('The torrent wait'))
  const next: DelayProfile = { preferredProtocol: preferred, usenetMinutes: Number(usenet), torrentMinutes: Number(torrent), bypassIfHighestQuality: bypass }
  const changed = !!saved && (next.preferredProtocol !== saved.preferredProtocol || next.usenetMinutes !== saved.usenetMinutes || next.torrentMinutes !== saved.torrentMinutes || next.bypassIfHighestQuality !== saved.bypassIfHighestQuality)
  const on = Number(usenet) > 0 || Number(torrent) > 0

  async function save() {
    if (usenetError || torrentError) return
    setBusy(true)
    try {
      const p = await api.putDelayProfile(next)
      setSaved(p)
      toast.success(p.usenetMinutes > 0 || p.torrentMinutes > 0 ? t('Saved: automatic searches wait for the better protocol.') : t('Saved: automatic searches no longer wait.'))
    } catch (e) {
      toast.error(t('Not saved: {error}', { error: e instanceof Error ? e.message : String(e) }))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card">
      <h2><TipLabel help="quality#wait-for-a-better-protocol">{t('Wait for a better protocol')}</TipLabel></h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t("Mediarium's own searches can hold a release back for a while, so a copy from the protocol you like better has time to turn up. The wait counts from when the release was posted. A search or download you start yourself never waits. With 0 minutes for both, nothing waits.")}
      </p>
      <label className="field" style={{ maxWidth: 320 }}>
        <span>{t('Preferred protocol')}</span>
        <select value={preferred} disabled={!saved || busy} onChange={(e) => setPreferred(e.target.value === 'torrent' ? 'torrent' : 'usenet')}>
          <option value="usenet">{t('Usenet')}</option>
          <option value="torrent">{t('Torrent')}</option>
        </select>
        <small>{t('Between two equally good releases, this one wins.')}</small>
      </label>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px 32px', margin: '12px 0' }}>
        <div>
          <label htmlFor="delay-usenet" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
            {tx('Wait for Usenet releases {box} minutes', {
              box: <input id="delay-usenet" inputMode="numeric" value={usenet} disabled={!saved || busy} onChange={(e) => setUsenet(e.target.value)} aria-invalid={usenetError ? true : undefined} style={{ width: 90 }} />,
            })}
          </label>
          {usenetError ? <small style={{ color: 'var(--danger)' }}>{usenetError}</small> : <small className="field-hint">{waitText(Number(usenet))}</small>}
        </div>
        <div>
          <label htmlFor="delay-torrent" style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
            {tx('Wait for torrent releases {box} minutes', {
              box: <input id="delay-torrent" inputMode="numeric" value={torrent} disabled={!saved || busy} onChange={(e) => setTorrent(e.target.value)} aria-invalid={torrentError ? true : undefined} style={{ width: 90 }} />,
            })}
          </label>
          {torrentError ? <small style={{ color: 'var(--danger)' }}>{torrentError}</small> : <small className="field-hint">{waitText(Number(torrent))}</small>}
        </div>
      </div>
      <Switch
        checked={bypass}
        onChange={setBypass}
        disabled={!saved || busy}
        label={t("Don't wait for the best quality")}
        description={t("A {protocol} release that is already the best quality the title's profile allows is taken at once.",{ protocol: preferred === 'usenet' ? t('Usenet') : t('torrent') })}
      />
      {on && saved && (
        <p className="field-hint">
          {t('A release that is held back shows on the Wanted page as "Waiting for a better protocol". It is taken when its wait is over.')}
        </p>
      )}
      <p style={{ marginBottom: 0 }}>
        <button className="primary" disabled={!changed || busy || !!usenetError || !!torrentError} onClick={() => void save()}>
          {busy ? t('Saving…') : t('Save')}
        </button>
      </p>
    </section>
  )
}
