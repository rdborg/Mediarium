import { useCallback, useEffect, useState } from 'react'
import { api, type QuietState } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'
import { locale, t, tx } from '../i18n'

// Minutes from now until 8:00 the next morning, in this browser's time.
function untilMorning(): number {
  const now = new Date()
  const next = new Date(now)
  next.setHours(8, 0, 0, 0)
  if (next <= now) next.setDate(next.getDate() + 1)
  return Math.max(1, Math.round((next.getTime() - now.getTime()) / 60000))
}

// Settings > Finding releases > Automatic searching: hold the heavy background work for a while.
export default function QuietForAWhile() {
  const toast = useToast()
  const [state, setState] = useState<QuietState | null>(null)
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => api.getQuiet().then(setState).catch(() => undefined), [])
  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(), 60000)
    return () => window.clearInterval(timer)
  }, [load])

  async function set(minutes: number) {
    setBusy(true)
    try {
      const s = await api.setQuiet(minutes)
      setState(s)
      toast.success(s.active ? t('Quiet for a while. The heavy background work waits.') : t('Back to normal.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!state) return null
  const until = state.active && state.until ? new Date(state.until).toLocaleString(locale(), { weekday: 'short', hour: '2-digit', minute: '2-digit' }) : ''
  return (
    <div className="quiet-while">
      <div className="inline-field">
        <Icon name="moon" size={15} />
        <strong>{t('Quiet for a while')}</strong>
        {state.active ? (
          <>
            <span>{tx('The heavy background work waits until {time}.', { time: <strong>{until}</strong> })}</span>
            <button type="button" className="btn-sm" disabled={busy} onClick={() => void set(0)}>
              {t('Back to normal')}
            </button>
          </>
        ) : (
          <>
            <button type="button" className="btn-sm" disabled={busy} onClick={() => void set(60)}>
              {t('1 hour')}
            </button>
            <button type="button" className="btn-sm" disabled={busy} onClick={() => void set(240)}>
              {t('4 hours')}
            </button>
            <button type="button" className="btn-sm" disabled={busy} onClick={() => void set(untilMorning())}>
              {t('Until morning')}
            </button>
          </>
        )}
      </div>
      <p className="field-hint">{t('Holds the searches, library refreshes, subtitle sweep and picture fetching, for example while a film plays from a small server. Downloads, imports, backups and anything you start yourself carry on.')}</p>
    </div>
  )
}
