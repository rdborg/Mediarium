import { useEffect, useState } from 'react'
import { api } from '../api'
import { useModules } from '../ModulesContext'
import { useToast } from './Toast'
import { t, tx } from '../i18n'

// Settings > Library and files: an optional Hardcover token for better
// book series and release dates. A Hardcover token belongs to one account and
// can't be shared, so none comes with Mediarium; without one, Open Library is used.
export default function HardcoverCard() {
  const toast = useToast()
  const { on } = useModules()
  const [set, setSet] = useState<boolean | null>(null)
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getHardcover()
      .then((s) => setSet(s.set))
      .catch(() => setSet(false))
  }, [])

  if (!on('ebooks') && !on('audiobooks')) return null

  async function save(value: string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putHardcover(value.trim())
      setSet(s.set)
      setToken('')
      toast.success(s.set ? (s.username ? t('Hardcover connected as {name}.', { name: s.username }) : t('Hardcover connected.')) : t('Hardcover token removed. Books use Open Library alone.'))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className={`card service-card ${set ? 'is-included' : 'is-optional'}`}>
      <h2>
        {t('Hardcover (books)')} <span className="req-badge optional">{t('Optional')}</span>{' '}
        {set !== null && <span className={`badge ${set ? 'downloaded' : 'missing'}`}>{set ? t('connected') : t('not set up')}</span>}
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t("Book details and series come from Open Library, which needs nothing from you. Hardcover knows more series and when the next book comes out, so with it following a series and the books calendar work better. Its token belongs to your own account, so it can't come built in.")}
      </p>
      <ol style={{ margin: '8px 0 14px', paddingLeft: 20 }}>
        <li style={{ marginBottom: 4 }}>
          {tx('Sign in (or make a free account) at {link}.', {
            link: (
              <a href="https://hardcover.app" target="_blank" rel="noreferrer">
                hardcover.app
              </a>
            ),
          })}
        </li>
        <li style={{ marginBottom: 4 }}>
          {tx('Open {link} and copy your token.', {
            link: (
              <a href="https://hardcover.app/account/api" target="_blank" rel="noreferrer">
                {t('Settings, Hardcover API')}
              </a>
            ),
          })}
        </li>
        <li>{t('Paste it here and press Save. Mediarium checks it with Hardcover first.')}</li>
      </ol>
      <div className="grid-form">
        <label>
          {set ? t('Replace the token') : t('Hardcover token')}
          <input value={token} onChange={(e) => setToken(e.target.value)} placeholder="Bearer eyJ…" autoComplete="off" spellCheck={false} disabled={busy} />
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" disabled={busy || token.trim() === ''} onClick={() => void save(token)}>
            {busy ? t('Checking…') : t('Save')}
          </button>
          {set && (
            <button disabled={busy} onClick={() => void save('')}>
              {t('Remove the token')}
            </button>
          )}
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </section>
  )
}
