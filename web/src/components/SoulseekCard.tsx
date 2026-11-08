import { useEffect, useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import TipLabel from './TipLabel'
import { useToast } from './Toast'
import { t } from '../i18n'

interface SoulseekState {
  url: string
  hasKey: boolean
  folder: string
  version?: string
}

// Settings > Downloading > Download rules: Soulseek for music through your
// own slskd (#143). Albums' Choose a release then also lists the folders
// Soulseek users share, and a grab downloads through slskd.
export default function SoulseekCard() {
  const toast = useToast()
  const [state, setState] = useState<SoulseekState | null>(null)
  const [url, setUrl] = useState('')
  const [key, setKey] = useState('')
  const [folder, setFolder] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getSoulseek()
      .then((s) => {
        setState(s)
        setUrl(s.url)
        setFolder(s.folder)
      })
      .catch(() => undefined)
  }, [])

  async function send(body: { url?: string; apiKey?: string; folder?: string; remove?: boolean }, done: (s: SoulseekState) => string) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putSoulseek(body)
      setState(s)
      setUrl(s.url)
      setFolder(s.folder)
      setKey('')
      toast.success(done(s))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const linked = !!state?.url && !!state.hasKey && !!state.folder
  return (
    <fieldset className="group usenet">
      <legend>
        <Icon name="music" size={14} />{' '}
        <TipLabel text={t('For music: Choose a release on an album also lists the folders Soulseek users share, and a download goes through your own slskd. Mediarium never connects to Soulseek itself.')} learnMore="downloads#soulseek-for-music">
          {t('Soulseek through slskd (optional)')}
        </TipLabel>
      </legend>
      <div className="grid-form">
        <label>
          {t('slskd address')}
          <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="http://192.168.1.10:5030" spellCheck={false} autoComplete="off" disabled={busy} />
        </label>
        <label>
          {state?.hasKey ? t('Replace the API key') : t('slskd API key')}
          <input value={key} onChange={(e) => setKey(e.target.value)} placeholder={state?.hasKey ? t('Leave empty to keep the saved key') : ''} spellCheck={false} autoComplete="off" disabled={busy} />
          <small className="field-hint">{t('A key with the readwrite role, from web > authentication > api_keys in slskd.yml.')}</small>
        </label>
        <label>
          {t('slskd downloads folder')}
          <input value={folder} onChange={(e) => setFolder(e.target.value)} placeholder="/data/slskd/downloads" spellCheck={false} autoComplete="off" disabled={busy} />
          <small className="field-hint">{t('The folder slskd downloads into, as Mediarium sees it. Mount the same folder into both containers.')}</small>
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button
            className="primary"
            disabled={busy || url.trim() === '' || folder.trim() === '' || (!state?.hasKey && key.trim() === '')}
            onClick={() => void send({ url: url.trim(), apiKey: key.trim(), folder: folder.trim() }, (s) => t('slskd {version} connected.', { version: s.version ?? '' }))}
          >
            {busy ? t('Checking…') : t('Save')}
          </button>
          {linked && (
            <button disabled={busy} onClick={() => void send({ remove: true }, () => t('Soulseek is switched off.'))}>
              {t('Remove')}
            </button>
          )}
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </fieldset>
  )
}
