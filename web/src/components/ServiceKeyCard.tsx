import { useState, type ReactNode } from 'react'
import { api } from '../api'
import { useToast } from './Toast'
import TestButton from './TestButton'

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

  async function useSharedKey() {
    setBusy(true)
    try {
      await api.putSettings({ [FIELD[service]]: '' })
      toast.success(`${title}: back on the shared key.`)
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function save() {
    setBusy(true)
    try {
      await api.putSettings({ [FIELD[service]]: key.trim() })
      setKey('')
      toast.success(`${title}: saved.`)
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
                  Using <strong>your own key</strong> for {title}. <button className="btn-sm" disabled={busy} onClick={() => void useSharedKey()}>Switch back to the shared key</button>
                </>
              ) : (
                <>
                  Connected with the key that comes with Mediarium, so there is nothing to set up. It is shared by everyone, so if you ever reach its limit you can switch to your own free key below.
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
                  <input value={key} onChange={(e) => setKey(e.target.value)} placeholder={placeholder} autoComplete="off" spellCheck={false} />
                </label>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                  <button className="primary" disabled={!key.trim() || busy} onClick={save}>
                    Save
                  </button>
                  <TestButton label="Test it" disabled={!key.trim()} run={() => api.testService({ service, key: key.trim() })} />
                </div>
              </div>
            </details>
          </>
        ) : (
          <div className="notice notice-info">Nothing to set up: this release of Mediarium already includes a key for {title}.</div>
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
              <input value={key} onChange={(e) => setKey(e.target.value)} placeholder={placeholder} autoComplete="off" spellCheck={false} />
            </label>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
              <button className="primary" disabled={!key.trim() || busy} onClick={save}>
                Save
              </button>
              <TestButton
                label="Test it"
                disabled={!key.trim() && !configured}
                run={() => api.testService({ service, key: key.trim() })}
              />
            </div>
          </div>
        </>
      )}
      {children}
    </section>
  )
}
