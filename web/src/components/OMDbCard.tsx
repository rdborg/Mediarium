import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from './Toast'
import { t, tx } from '../i18n'

// Settings > Info, lists and subtitles: an optional OMDb key, for the IMDb,
// Rotten Tomatoes and Metacritic scores on title pages and Discover cards (#169).
export default function OMDbCard() {
  const toast = useToast()
  const [set, setSet] = useState<boolean | null>(null)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getOMDb()
      .then((s) => setSet(s.set))
      .catch(() => setSet(false))
  }, [])

  async function send(body: { key?: string; remove?: boolean }, done: string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putOMDb(body)
      setSet(s.set)
      setKey('')
      toast.success(done)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className={`card service-card ${set ? 'is-included' : 'is-optional'}`}>
      <h2>
        {t('Scores from IMDb, Rotten Tomatoes and Metacritic')} <span className="req-badge optional">{t('Optional')}</span>{' '}
        {set !== null && <span className={`badge ${set ? 'downloaded' : 'missing'}`}>{set ? t('connected') : t('not set up')}</span>}
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('TMDB’s score is always shown. With a free OMDb key, title pages also show the IMDb, Rotten Tomatoes and Metacritic scores, and Discover cards the Rotten Tomatoes one. Only the title’s IMDb id is sent to OMDb.')}
      </p>
      <ol style={{ margin: '8px 0 14px', paddingLeft: 20 }}>
        <li style={{ marginBottom: 4 }}>
          {tx('Ask for a free key at {link}: pick FREE, fill in your email and send.', {
            link: (
              <a href="https://www.omdbapi.com/apikey.aspx" target="_blank" rel="noreferrer">
                omdbapi.com
              </a>
            ),
          })}
        </li>
        <li style={{ marginBottom: 4 }}>{t('OMDb emails you the key with a link. Open the link, or the key doesn’t work.')}</li>
        <li>{t('Paste the key here and press Save. Mediarium checks it with OMDb first.')}</li>
      </ol>
      <div className="grid-form">
        <label>
          {set ? t('Replace the key') : t('OMDb key')}
          <input value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" spellCheck={false} disabled={busy} />
          <small className="field-hint">{t('A free key allows 1,000 lookups a day. Scores are kept for a week, and Discover cards only use part of the day’s lookups.')}</small>
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" disabled={busy || key.trim() === ''} onClick={() => void send({ key: key.trim() }, t('OMDb connected. Scores show on title pages from now on.'))}>
            {busy ? t('Checking…') : t('Save')}
          </button>
          {set && (
            <button disabled={busy} onClick={() => void send({ remove: true }, t('OMDb key removed.'))}>
              {t('Remove the key')}
            </button>
          )}
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </section>
  )
}
