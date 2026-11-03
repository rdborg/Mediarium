import { useEffect, useState } from 'react'
import { api } from '../api'
import { useModules } from '../ModulesContext'
import { useToast } from './Toast'
import { t, tx } from '../i18n'

// Settings > Info, lists and subtitles. The key belongs to the user's own
// Google Cloud project. The language of the results is the movie and show
// info language on this same page, so there is no second language choice.
export default function GoogleBooksCard() {
  const toast = useToast()
  const { on } = useModules()
  const [set, setSet] = useState<boolean | null>(null)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getGoogleBooks()
      .then((s) => setSet(s.set))
      .catch(() => setSet(false))
  }, [])

  if (!on('ebooks') && !on('audiobooks')) return null

  async function save(value: string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putGoogleBooks(value.trim())
      setSet(s.set)
      setKey('')
      toast.success(s.set ? t('Google Books connected.') : t('Google Books key removed.'))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className={`card service-card ${set ? 'is-included' : 'is-optional'}`}>
      <h2>
        {t('Google Books (books)')} <span className="req-badge optional">{t('Optional')}</span>{' '}
        {set !== null && <span className={`badge ${set ? 'downloaded' : 'missing'}`}>{set ? t('Key saved') : t('No key')}</span>}
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('Book search asks Google Books, with no API key, when a result has no cover or no description. A book only Google Books knows is added with its details and its cover. Save a key here if you reach Google\'s limit.')}
      </p>
      <ol style={{ margin: '8px 0 14px', paddingLeft: 20 }}>
        <li style={{ marginBottom: 4 }}>
          {tx('In {link}, create a project if you need one and switch on the Books API.', {
            link: (
              <a href="https://console.cloud.google.com/apis/library/books.googleapis.com" target="_blank" rel="noreferrer">
                Google Cloud
              </a>
            ),
          })}
        </li>
        <li style={{ marginBottom: 4 }}>
          {tx('Create an API key under {link} and copy it.', {
            link: (
              <a href="https://console.cloud.google.com/apis/credentials" target="_blank" rel="noreferrer">
                {t('APIs and services, Credentials')}
              </a>
            ),
          })}
        </li>
        <li>{t('Paste it here and press Save. Mediarium checks it with Google Books first.')}</li>
      </ol>
      <div className="grid-form">
        <label>
          {set ? t('Replace the key') : t('Google Books API key')}
          <input value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" spellCheck={false} disabled={busy} />
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" disabled={busy || key.trim() === ''} onClick={() => void save(key)}>
            {busy ? t('Checking…') : t('Save')}
          </button>
          {set && (
            <button disabled={busy} onClick={() => void save('')}>
              {t('Remove the key')}
            </button>
          )}
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </section>
  )
}
