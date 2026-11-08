import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from './Toast'
import Switch from './Switch'
import { t, tx } from '../i18n'

// Settings > Info, lists and subtitles: an optional TheTVDB key, as a second
// source for shows whose TMDB page is missing seasons or episodes. TMDB stays
// the source of every show; TheTVDB only fills in what TMDB doesn't list.
export default function TheTVDBCard() {
  const toast = useToast()
  const [state, setState] = useState<{ set: boolean; fill: boolean } | null>(null)
  const [key, setKey] = useState('')
  const [pin, setPin] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getTheTVDB()
      .then(setState)
      .catch(() => setState({ set: false, fill: false }))
  }, [])

  async function send(body: { key?: string; pin?: string; fill?: boolean; remove?: boolean }, done: (s: { set: boolean; fill: boolean }) => string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putTheTVDB(body)
      setState(s)
      setKey('')
      setPin('')
      toast.success(done(s))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const set = !!state?.set
  return (
    <section className={`card service-card ${set ? 'is-included' : 'is-optional'}`}>
      <h2>
        {t('TheTVDB (shows)')} <span className="req-badge optional">{t('Optional')}</span>{' '}
        {state !== null && <span className={`badge ${set ? 'downloaded' : 'missing'}`}>{set ? t('connected') : t('not set up')}</span>}
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('Shows come from TMDB. A few TMDB pages are missing seasons or episodes that TheTVDB has, Formula 1 for one. With your own TheTVDB key, Mediarium fills those in when it refreshes a show, but only where both count the episodes the same way. TMDB stays the source for everything else.')}
      </p>
      <ol style={{ margin: '8px 0 14px', paddingLeft: 20 }}>
        <li style={{ marginBottom: 4 }}>
          {tx('Sign in (or make a free account) at {link}.', {
            link: (
              <a href="https://thetvdb.com" target="_blank" rel="noreferrer">
                thetvdb.com
              </a>
            ),
          })}
        </li>
        <li style={{ marginBottom: 4 }}>
          {tx('Open {link} and make a key.', {
            link: (
              <a href="https://thetvdb.com/dashboard/account/apikey" target="_blank" rel="noreferrer">
                {t('Dashboard, API keys')}
              </a>
            ),
          })}
        </li>
        <li>{t('Paste it here and press Save. A user key also needs your subscriber PIN. Mediarium checks the key with TheTVDB first.')}</li>
      </ol>
      <div className="grid-form">
        <div className="form-cols">
          <label>
            {set ? t('Replace the key') : t('TheTVDB API key')}
            <input value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" spellCheck={false} disabled={busy} />
          </label>
          <label>
            {t('Subscriber PIN (if your key needs one)')}
            <input value={pin} onChange={(e) => setPin(e.target.value)} autoComplete="off" spellCheck={false} disabled={busy} />
          </label>
        </div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" disabled={busy || key.trim() === ''} onClick={() => void send({ key: key.trim(), pin: pin.trim(), fill: true }, () => t('TheTVDB connected. Shows fill in from it at their next refresh.'))}>
            {busy ? t('Checking…') : t('Save')}
          </button>
          {set && (
            <button disabled={busy} onClick={() => void send({ remove: true }, () => t('TheTVDB key removed. Shows use TMDB alone.'))}>
              {t('Remove the key')}
            </button>
          )}
        </div>
        {set && (
          <Switch
            checked={!!state?.fill}
            disabled={busy}
            onChange={(fill) => void send({ fill }, (s) => (s.fill ? t('Shows fill in from TheTVDB again.') : t('Shows no longer fill in from TheTVDB.')))}
            label={t('Fill in shows from TheTVDB')}
            showState
            tip={t('Episodes added this way are monitored like any new episode of a monitored show. Switch monitoring off for old seasons you do not want.')}
            learnMore="library#thetvdb-as-a-second-source"
          />
        )}
        {error && <p className="error-text">{error}</p>}
      </div>
    </section>
  )
}
