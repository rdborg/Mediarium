import { useEffect, useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import Switch from './Switch'
import TipLabel from './TipLabel'
import { useToast } from './Toast'
import { timeAgo } from '../format'
import { t, st } from '../i18n'

interface ProwlarrState {
  url: string
  hasKey: boolean
  sync: boolean
  linked: number
  last?: { at: string; ok: boolean; message: string }
}

// Settings > Indexers & Search: a link to a running Prowlarr (#134). Every
// indexer Prowlarr has is added here as Prowlarr's own feed for it, and kept
// in step every few hours, so the sites that only Prowlarr can do work too.
export default function ProwlarrLinkCard({ onSynced }: { onSynced: () => void }) {
  const toast = useToast()
  const [state, setState] = useState<ProwlarrState | null>(null)
  const [url, setUrl] = useState('')
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getProwlarr()
      .then((s) => {
        setState(s)
        setUrl(s.url)
      })
      .catch(() => undefined)
  }, [])

  async function run(work: () => Promise<ProwlarrState>, done: (s: ProwlarrState) => string) {
    setBusy(true)
    setError('')
    try {
      const s = await work()
      setState(s)
      setUrl(s.url)
      setKey('')
      toast.success(done(s))
      onSynced()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const linked = !!state?.url && !!state.hasKey
  return (
    <fieldset className="group usenet prowlarr-card">
      <legend>
        <Icon name="share" size={14} />{' '}
        <TipLabel text={t('Prowlarr can do some sites that Mediarium can\'t. Link it and each of its indexers is added here as Prowlarr\'s own feed for it, kept in step every few hours.')} learnMore="indexers#a-running-prowlarr">
          {t('Prowlarr (optional)')}
        </TipLabel>
      </legend>
      {linked && state?.last && (
        <p className={`field-hint ${state.last.ok ? '' : 'error-text'}`} style={{ marginTop: 0 }}>
          {st(state.last.message)} <small>({timeAgo(state.last.at)})</small>
        </p>
      )}
      <div className="grid-form">
        <label>
          {t('Prowlarr address')}
          <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://192.168.1.10:9696" spellCheck={false} autoComplete="off" disabled={busy} />
        </label>
        <label>
          {state?.hasKey ? t('Replace the API key') : t('Prowlarr API key')}
          <input value={key} onChange={(e) => setKey(e.target.value)} placeholder={state?.hasKey ? t('Leave empty to keep the saved key') : ''} spellCheck={false} autoComplete="off" disabled={busy} />
          <small className="field-hint">{t('In Prowlarr under Settings > General.')}</small>
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" disabled={busy || url.trim() === '' || (!state?.hasKey && key.trim() === '')} onClick={() => void run(() => api.putProwlarr({ url: url.trim(), apiKey: key.trim(), sync: state?.sync ?? true }), (s) => t('Prowlarr linked: {n} indexers.', { n: s.linked }))}>
            {busy ? t('Checking…') : linked ? t('Save') : t('Link Prowlarr')}
          </button>
          {linked && (
            <>
              <button disabled={busy} onClick={() => void run(() => api.syncProwlarr(), () => t('Read Prowlarr again.'))}>
                {t('Sync now')}
              </button>
              <button disabled={busy} onClick={() => void run(() => api.putProwlarr({ remove: true }), () => t('Prowlarr unlinked. Its indexers stay until you remove them.'))}>
                {t('Unlink')}
              </button>
            </>
          )}
        </div>
        {linked && (
          <Switch
            checked={!!state?.sync}
            disabled={busy}
            onChange={(sync) => void run(() => api.putProwlarr({ url: url.trim(), sync }), (s) => (s.sync ? t('Prowlarr is kept in step again.') : t('Prowlarr is no longer kept in step.')))}
            label={t('Keep in step every few hours')}
            showState
          />
        )}
        {error && <p className="error-text">{error}</p>}
      </div>
    </fieldset>
  )
}
