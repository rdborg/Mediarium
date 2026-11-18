import { useCallback, useEffect, useState } from 'react'
import { api, type SessionInfo } from '../api'
import { locale, t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'

function how(method: string): string {
  switch (method) {
    case 'oidc':
      return t('Single sign-on')
    case 'ldap':
      return t('Directory')
    case 'passkey':
      return t('Passkey')
    case 'proxy':
      return t('Sign-in proxy')
    default:
      return t('Password')
  }
}

// Settings > People and access > Security: where this account is signed in, and a way to end every
// other browser or app at once (a lost phone, a shared computer).
export default function SessionsCard() {
  const toast = useToast()
  const [list, setList] = useState<SessionInfo[] | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    api
      .sessions()
      .then(setList)
      .catch(() => setList(null))
  }, [])
  useEffect(load, [load])

  if (!list) return null
  const others = list.filter((s) => !s.current).length

  async function endOthers() {
    setBusy(true)
    try {
      const { ended } = await api.endOtherSessions()
      toast.success(ended > 0 ? t('Signed out {n} other devices.', { n: ended }) : t('No other devices were signed in.'))
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group">
      <legend>
        <Icon name="shield" size={14} /> {t('Signed-in devices')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Every browser or app that is signed in to your account right now. A sign-in lasts 7 days. If you do not know one of them, sign out the others and change your password.')}
      </p>
      <ul style={{ margin: '0 0 12px', paddingLeft: 18 }}>
        {list.map((s, i) => (
          <li key={i}>
            {how(s.method)} · {t('signed in {when}', { when: new Date(s.createdAt).toLocaleString(locale()) })}
            {s.current && <strong> · {t('This device')}</strong>}
          </li>
        ))}
      </ul>
      <button onClick={() => void endOthers()} disabled={busy || others === 0}>
        {t('Sign out all other devices')}
      </button>
    </fieldset>
  )
}
