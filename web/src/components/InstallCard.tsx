import { useEffect, useState } from 'react'
import Icon from './Icon'
import { currentInstallKind, installNow, onInstallChange } from '../pwa'
import { t } from '../i18n'

// Settings > General > Install as an app. A browser that can install shows a
// button; Safari has no button, so it shows the steps. Nothing shows once
// Mediarium already runs as an app.
export default function InstallCard() {
  const [kind, setKind] = useState(currentInstallKind)
  useEffect(() => onInstallChange(() => setKind(currentInstallKind())), [])
  if (kind === 'installed') return null

  return (
    <fieldset className="group">
      <legend>
        <Icon name="download" size={14} /> {t('Install as an app')}
      </legend>
      {kind === 'prompt' && (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 14 }}>
          <button className="primary btn-with-icon" style={{ whiteSpace: 'nowrap' }} onClick={() => void installNow()}>
            <Icon name="download" size={15} /> {t('Install app')}
          </button>
          <span className="install-line">{t('Opens in its own window, with its own icon.')}</span>
        </div>
      )}
      {kind === 'iphone' && <p className="install-line">{t('Tap the Share button, then Add to Home Screen.')}</p>}
      {kind === 'ipad' && <p className="install-line">{t('Tap Share at the top, then Add to Home Screen.')}</p>}
      {kind === 'mac-safari' && <p className="install-line">{t('In Safari, choose File, then Add to Dock.')}</p>}
      {kind === 'menu' && <p className="install-line">{t('Open the browser menu and choose Install app.')}</p>}
      {kind === 'insecure' && <p className="install-line">{t('Installing needs an https address.')}</p>}
    </fieldset>
  )
}
