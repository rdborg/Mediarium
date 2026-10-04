import Icon from './Icon'
import { useAutosaveSetting } from '../useAutosave'

const HOURS = Array.from({ length: 24 }, (_, h) => h)

// Settings > Connections > Notifications: hours when everyday messages wait.
// Problems are always sent straight away.
export default function QuietHours() {
  const quiet = useAutosaveSetting<string>(
    (s) => s.notifyQuietHours ?? '',
    (v) => ({ notifyQuietHours: v }),
    (v) => (v === '' ? 'Saved: messages are sent at any time.' : `Saved: quiet from ${v.split('-')[0]}:00 to ${v.split('-')[1]}:00.`),
    '',
  )
  const on = quiet.value !== ''
  const [from, to] = on ? quiet.value.split('-').map(Number) : [23, 7]
  const busy = !quiet.loaded || quiet.saving
  return (
    <fieldset className="group folders span-all">
      <legend>
        <Icon name="moon" size={14} /> Quiet hours
      </legend>
      <div className="inline-field">
        <label className="inline-field">
          <input type="checkbox" checked={on} disabled={busy} onChange={(e) => void quiet.change(e.target.checked ? '23-7' : '')} />
          Hold everyday messages from
        </label>
        <select aria-label="From" value={from} disabled={busy || !on} onChange={(e) => void quiet.change(`${e.target.value}-${to === Number(e.target.value) ? (to + 1) % 24 : to}`)}>
          {HOURS.map((h) => (
            <option key={h} value={h}>
              {h}:00
            </option>
          ))}
        </select>
        to
        <select aria-label="To" value={to} disabled={busy || !on} onChange={(e) => void quiet.change(`${from === Number(e.target.value) ? (from + 23) % 24 : from}-${e.target.value}`)}>
          {HOURS.map((h) => (
            <option key={h} value={h}>
              {h}:00
            </option>
          ))}
        </select>
      </div>
      <p className="field-hint">
        Messages like &ldquo;ready to watch&rdquo; wait and arrive together when the quiet hours end. A failed download, a broken connection or a decision to make is always sent straight away. The hours are the server&apos;s time.
      </p>
    </fieldset>
  )
}
