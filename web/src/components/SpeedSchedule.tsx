import { useAutosaveSetting } from '../useAutosave'
import { locale, t } from '../i18n'
import type { SpeedRule } from '../api'

const HOURS = Array.from({ length: 24 }, (_, h) => h)
const SPEEDS = [0, 1, 2, 5, 10, 20, 50, 100]

// Short weekday names in the screen language, Monday first. 2024-01-07 was a Sunday.
function weekdayNames(): { day: number; name: string }[] {
  const fmt = new Intl.DateTimeFormat(locale(), { weekday: 'short' })
  return [1, 2, 3, 4, 5, 6, 0].map((day) => ({ day, name: fmt.format(new Date(2024, 0, 7 + day)) }))
}

// Settings > Downloading > Download rules: lines of "on these days, at these hours, limit
// to this speed". The first line that fits the moment sets the limit; outside
// every line, the plain limit above applies.
export default function SpeedSchedule() {
  const sched = useAutosaveSetting<SpeedRule[]>(
    (s) => s.speedSchedule ?? [],
    (v) => ({ speedSchedule: v }),
    (v) => (v.length === 0 ? t('Saved: no speed schedule.') : t('Saved: the speed schedule has {n} lines.', { n: v.length })),
    [],
  )
  const names = weekdayNames()
  const busy = !sched.loaded || sched.saving
  const edit = (i: number, patch: Partial<SpeedRule>) => void sched.change(sched.value.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  // No days listed means every day, so the buttons all show as pressed then.
  const toggleDay = (i: number, day: number) => {
    const now = sched.value[i].days.length === 0 ? [0, 1, 2, 3, 4, 5, 6] : sched.value[i].days
    const next = now.includes(day) ? now.filter((d) => d !== day) : [...now, day].sort()
    if (next.length === 0) return // at least one day stays
    edit(i, { days: next.length === 7 ? [] : next })
  }
  return (
    <div className="speed-schedule">
      <p className="field-hint" style={{ marginTop: 0 }}>
        {t('A schedule changes the limit by day and hour, for example slow in the evening while people watch and unlimited at night. The first line that fits the time is used; outside every line the limit above applies.')}
      </p>
      {sched.value.map((r, i) => (
        <div key={i} className="speed-rule">
          <div className="seg speed-days" role="group" aria-label={t('Days')}>
            {names.map(({ day, name }) => (
              <button
                key={day}
                type="button"
                className={r.days.length === 0 || r.days.includes(day) ? 'active' : ''}
                aria-pressed={r.days.length === 0 || r.days.includes(day)}
                disabled={busy}
                onClick={() => toggleDay(i, day)}
              >
                {name}
              </button>
            ))}
          </div>
          <select aria-label={t('From')} value={r.from} disabled={busy} onChange={(e) => edit(i, { from: Number(e.target.value) })}>
            {HOURS.map((h) => (
              <option key={h} value={h}>
                {h}:00
              </option>
            ))}
          </select>
          <span aria-hidden="true">–</span>
          <select aria-label={t('To')} value={r.to} disabled={busy} onChange={(e) => edit(i, { to: Number(e.target.value) })}>
            {HOURS.map((h) => (
              <option key={h} value={h}>
                {h}:00
              </option>
            ))}
          </select>
          <select aria-label={t('Limit')} value={r.limitMB} disabled={busy} onChange={(e) => edit(i, { limitMB: Number(e.target.value) })}>
            {Array.from(new Set([...SPEEDS, r.limitMB])).sort((a, b) => a - b).map((n) => (
              <option key={n} value={n}>
                {n === 0 ? t('no limit') : `${n} MB/s`}
              </option>
            ))}
          </select>
          <button type="button" className="btn-sm" disabled={busy} onClick={() => void sched.change(sched.value.filter((_, j) => j !== i))}>
            {t('Remove')}
          </button>
        </div>
      ))}
      <button
        type="button"
        className="btn-sm"
        disabled={busy || sched.value.length >= 14}
        onClick={() => void sched.change([...sched.value, { days: [], from: 0, to: 7, limitMB: 0 }])}
      >
        {t('Add a line')}
      </button>
    </div>
  )
}
