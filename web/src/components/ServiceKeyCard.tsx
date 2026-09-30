import { useState, type ReactNode } from 'react'
import { api } from '../api'
import { useToast } from './Toast'
import TestButton from './TestButton'
import { FieldError, FormProblem, useValidation } from '../useValidation'
import { apiKey as apiKeyCheck, firstError, required as requiredCheck } from '../validate'

export type KeyService = 'tmdb' | 'opensubtitles' | 'trakt'

const FIELD: Record<KeyService, 'tmdbApiKey' | 'openSubtitlesApiKey' | 'traktClientId'> = {
  tmdb: 'tmdbApiKey',
  opensubtitles: 'openSubtitlesApiKey',
  trakt: 'traktClientId',
}

// One card per third-party service Mediarium can use: what it is, what stops
// working without it, and how to get the key. On an official release that
// ships its own key the card just says so and asks for nothing.
export default function ServiceKeyCard({
  service,
  title,
  required,
  blurb,
  without,
  steps,
  fieldLabel,
  placeholder,
  builtIn,
  configured,
  onSaved,
  allowOverride,
  usingOwnKey,
  limitNote,
  children,
}: {
  service: KeyService
  title: string
  required: boolean
  blurb: ReactNode
  without: ReactNode
  steps: ReactNode[]
  fieldLabel: string
  placeholder: string
  builtIn: boolean
  configured: boolean
  onSaved: () => void
  // Shared key shipped with the app: let people swap in their own free key if they hit its limit.
  allowOverride?: boolean
  usingOwnKey?: boolean
  limitNote?: ReactNode
  children?: ReactNode
}) {
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const toast = useToast()
  // "TMDB (movie and show information)" reads better in a toast as just "TMDB".
  const shortTitle = title.replace(/\s*\(.*\)\s*$/, '')
  const emptyMessage = `Paste your ${fieldLabel} into the box first.`
  const v = useValidation({ key: firstError(requiredCheck(key, emptyMessage), apiKeyCheck(key)) })

  // The Test button also works with no new key typed when one is already saved.
  async function testKey() {
    const k = key.trim()
    if (k === '' && !configured) return { ok: false, message: emptyMessage }
    const bad = apiKeyCheck(k)
    if (bad) return { ok: false, message: bad }
    return api.testService({ service, key: k })
  }

  async function switchToSharedKey() {
    setBusy(true)
    try {
      await api.putSettings({ [FIELD[service]]: '' })
      toast.success(`${shortTitle} is back on the shared key.`)
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function save() {
    if (!v.attempt()) return
    setBusy(true)
    try {
      await api.putSettings({ [FIELD[service]]: key.trim() })
      setKey('')
      v.reset()
      toast.success(`${shortTitle} key saved.`)
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className={`card service-card ${builtIn ? 'is-included' : required ? 'is-required' : 'is-optional'}`}>
      <h2>
        {title}{' '}
        {builtIn ? (
          <span className="req-badge included">{allowOverride ? (usingOwnKey ? 'Your own key' : 'Connected') : 'Included'}</span>
        ) : (
          <span className={`req-badge ${required ? 'required' : 'optional'}`}>{required ? 'Required' : 'Optional'}</span>
        )}{' '}
        {builtIn ? null : (
          <span className={`badge ${configured ? 'downloaded' : 'missing'}`}>{configured ? 'connected' : 'not set up'}</span>
        )}
      </h2>
      <p style={{ color: 'var(--text-dim)' }}>{blurb}</p>
      {!builtIn && !configured && (
        <div className={`notice ${required ? 'notice-danger' : 'notice-warn'}`}>
          <strong>Without it:</strong> {without}
        </div>
      )}
      {builtIn ? (
        allowOverride ? (
          <>
            <div className="notice notice-info">
              {usingOwnKey ? (
                <>
                  Using <strong>your own key</strong> for {title}. <button className="btn-sm" disabled={busy} onClick={() => void switchToSharedKey()}>Switch back to the shared key</button>
                </>
              ) : (
                <>
                  Connected with the key that comes with Mediarium. If you hit its shared limit, use your own free key below.
                </>
              )}
            </div>
            {limitNote}
            <details className="own-key">
              <summary>{usingOwnKey ? 'Replace my key' : 'Reaching a limit? Use my own free key'}</summary>
              <ol style={{ margin: '8px 0 14px', paddingLeft: 20 }}>
                {steps.map((step, i) => (
                  <li key={i} style={{ marginBottom: 4 }}>
                    {step}
                  </li>
                ))}
              </ol>
              <div className="grid-form">
                <label>
                  {fieldLabel}
                  <input value={key} onChange={(e) => setKey(e.target.value)} placeholder={placeholder} autoComplete="off" spellCheck={false} {...v.bind('key', key, setKey)} />
                  <FieldError v={v} name="key" />
                </label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                  <button className="primary" disabled={busy} onClick={save}>
                    Save
                  </button>
                  <TestButton label="Test it" run={testKey} />
                </div>
                <FormProblem v={v} />
              </div>
            </details>
          </>
        ) : (
          <div className="notice notice-info">This version of Mediarium includes a key for {title}.</div>
        )
      ) : (
        <>
          <ol style={{ margin: '8px 0 14px', paddingLeft: 20 }}>
            {steps.map((step, i) => (
              <li key={i} style={{ marginBottom: 4 }}>
                {step}
              </li>
            ))}
          </ol>
          <div className="grid-form">
            <label>
              {configured ? `Replace ${fieldLabel}` : fieldLabel}
              <input value={key} onChange={(e) => setKey(e.target.value)} placeholder={placeholder} autoComplete="off" spellCheck={false} {...v.bind('key', key, setKey)} />
              <FieldError v={v} name="key" />
            </label>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
              <button className="primary" disabled={busy} onClick={save}>
                Save
              </button>
              <TestButton label="Test it" run={testKey} />
            </div>
            <FormProblem v={v} />
          </div>
        </>
      )}
      {children}
    </section>
  )
}
