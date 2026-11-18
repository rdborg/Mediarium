import { useEffect, useState } from 'react'
import { api, type IdleSignOut } from '../api'
import { plural, t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'

// The choices, in days. 0 is off. A sign-in lasts 7 days at most, so a longer
// limit would never apply.
const CHOICES = [0, 1, 2, 3, 5, 7]

// Settings > People and access > Security: sign a browser out when it has not
// been used for a while. Off until a number of days is chosen. Only for administrators.
export default function IdleSignOutCard() {
  const toast = useToast()
  const [state, setState] = useState<IdleSignOut | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getIdleSignOut()
      .then(setState)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  async function save(days: number) {
    setBusy(true)
    setError('')
    try {
      const s = await api.putIdleSignOut(days)
      setState(s)
      toast.success(
        s.days
          ? plural(s.days, 'Browsers are signed out after {n} day of not being used.', 'Browsers are signed out after {n} days of not being used.')
          : t('Browsers are no longer signed out for not being used.'),
      )
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const days = state?.days ?? 0
  const choices = CHOICES.includes(days) ? CHOICES : [...CHOICES, days].sort((a, b) => a - b)
  return (
    <fieldset className="group">
      <legend>
        <Icon name="shield" size={14} /> {t('Sign out idle browsers')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t("A browser that hasn't been used for this long is signed out and asks for the password again. A sign-in ends after 7 days anyway. API keys, calendar feeds and e-reader passwords are not affected.")}
      </p>
      <label style={{ display: 'grid', gap: 4, maxWidth: 260 }}>
        {t('Sign out after being idle for')}
        <select value={days} disabled={busy || state === null} onChange={(e) => void save(Number(e.target.value))}>
          {choices.map((n) => (
            <option key={n} value={n}>
              {n === 0 ? t('Off') : plural(n, '{n} day', '{n} days')}
            </option>
          ))}
        </select>
      </label>
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}
