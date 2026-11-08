import { useEffect } from 'react'
import { useToast } from './Toast'
import { t } from '../i18n'

// When a new version of the app is deployed, the service worker takes over by
// itself (see push-sw.js). The page that is already open still runs the old
// code, so offer a reload.
export default function UpdateNotice() {
  const toast = useToast()
  useEffect(() => {
    if (!('serviceWorker' in navigator)) return
    // Only a change of worker after one was already in charge is an update:
    // the first install also fires controllerchange.
    let had = navigator.serviceWorker.controller !== null
    const onChange = () => {
      if (!had) {
        had = true
        return
      }
      toast.info(t('A new version is ready.'), { label: t('Reload'), onClick: () => location.reload() })
    }
    navigator.serviceWorker.addEventListener('controllerchange', onChange)
    return () => navigator.serviceWorker.removeEventListener('controllerchange', onChange)
  }, [toast])
  return null
}
