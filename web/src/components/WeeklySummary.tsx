import { useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'
import { useAutosaveSetting } from '../useAutosave'
import { locale, t, tx } from '../i18n'

const HOURS = Array.from({ length: 24 }, (_, h) => h)

// Weekday names in the screen language, Monday first. 2024-01-07 was a Sunday.
function weekdays(): { day: number; name: string }[] {
  const fmt = new Intl.DateTimeFormat(locale(), { weekday: 'long' })
  return [1, 2, 3, 4, 5, 6, 0].map((day) => ({ day, name: fmt.format(new Date(2024, 0, 7 + day)) }))
}

// Settings > Media servers and apps > Notifications: a short message once a week with
// what came in, what failed and what is still missing. It goes to the targets
// that have "Weekly summary" ticked.
export default function WeeklySummary() {
  const toast = useToast()
  const [sending, setSending] = useState(false)
  const day = useAutosaveSetting<number>(
    (s) => s.digestDay ?? -1,
    (v) => ({ digestDay: v }),
    (v) => (v < 0 ? t('Saved: no weekly summary.') : t('Saved: the weekly summary is sent once a week.')),
    -1,
  )
  const hour = useAutosaveSetting<number>(
    (s) => s.digestHour ?? 9,
    (v) => ({ digestHour: v }),
    (v) => t('Saved: the weekly summary is sent at {hour}:00.', { hour: v }),
    9,
  )
  const on = day.value >= 0
  const busy = !day.loaded || day.saving || hour.saving
  async function sendNow() {
    setSending(true)
    try {
      await api.sendDigest()
      toast.success(t('Sent. It can take a moment to arrive.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSending(false)
    }
  }
  return (
    <fieldset className="group folders span-all">
      <legend>
        <Icon name="list" size={14} /> {t('Weekly summary')}
      </legend>
      <div className="inline-field">
        {tx('Send a summary {day} at {hour}', {
          day: (
            <select aria-label={t('Day')} value={day.value} disabled={busy} onChange={(e) => void day.change(Number(e.target.value))}>
              <option value={-1}>{t('never')}</option>
              {weekdays().map((d) => (
                <option key={d.day} value={d.day}>
                  {t('every {day}', { day: d.name })}
                </option>
              ))}
            </select>
          ),
          hour: (
            <select aria-label={t('Hour')} value={hour.value} disabled={busy || !on} onChange={(e) => void hour.change(Number(e.target.value))}>
              {HOURS.map((h) => (
                <option key={h} value={h}>
                  {h}:00
                </option>
              ))}
            </select>
          ),
        })}
        <button type="button" className="btn-sm" disabled={sending} onClick={() => void sendNow()}>
          {sending ? t('Sending…') : t('Send one now')}
        </button>
      </div>
      <p className="field-hint">
        {t('How many downloads arrived and failed this week, and how many movies and episodes are still missing. It goes to the targets that have Weekly summary ticked. The hour is the server\'s time.')}
      </p>
    </fieldset>
  )
}
