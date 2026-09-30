import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { firstError, numberRange } from '../validate'
import { useToast } from './Toast'

type Key = 'huntIntervalHours' | 'releaseCheckMinutes'

// One "every … units" number box. It saves by itself a moment after you stop
// typing, or when you leave the box, but only when the number is allowed.
function IntervalField({ id, before, unit, field, fallback, min, max, saved }: { id: string; before: string; unit: string; field: Key; fallback: number; min: number; max: number; saved: string }) {
  const toast = useToast()
  const [stored, setStored] = useState<number | null>(null)
  const [text, setText] = useState(String(fallback))
  const [busy, setBusy] = useState(false)
  const timer = useRef<number | undefined>(undefined)

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        const n = s[field] ?? fallback
        setStored(n)
        setText(String(n))
      })
      .catch(() => undefined)
    return () => window.clearTimeout(timer.current)
  }, [field, fallback])

  const error = firstError(text.trim() === '' && `Enter a number from ${min} to ${max}.`, numberRange(text, 'The number', { min, max }))

  async function save(value: string) {
    window.clearTimeout(timer.current)
    if (stored === null || error || Number(value) === stored) return
    setBusy(true)
    try {
      const s = await api.putSettings({ [field]: Number(value) })
      const n = s[field] ?? Number(value)
      setStored(n)
      setText(String(n))
      toast.success(saved.replace('{n}', String(n)))
    } catch (e) {
      toast.error(`Not saved: ${e instanceof Error ? e.message : String(e)}`)
      setText(String(stored))
    } finally {
      setBusy(false)
    }
  }

  function edit(value: string) {
    setText(value)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => void save(value), 1200)
  }

  return (
    <div>
      <label htmlFor={id} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
        {before}
        <input
          id={id}
          inputMode="numeric"
          value={text}
          disabled={stored === null || busy}
          onChange={(e) => edit(e.target.value)}
          onBlur={(e) => void save(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && void save(text)}
          aria-invalid={error ? true : undefined}
          style={{ width: 80 }}
        />
        {unit}
      </label>
      {error && <small style={{ color: 'var(--danger)' }}>{error}</small>}
    </div>
  )
}

// How often the two automatic loops run. Both take effect without a restart.
export default function AutomationIntervals() {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px 32px', marginTop: 16 }}>
      <IntervalField id="hunt-hours" before="Look for missing items every" unit="hours" field="huntIntervalHours" fallback={6} min={1} max={168} saved="Saved: Mediarium looks for missing items every {n} hours." />
      <IntervalField id="release-minutes" before="Check for new releases every" unit="minutes" field="releaseCheckMinutes" fallback={15} min={5} max={1440} saved="Saved: Mediarium checks for new releases every {n} minutes." />
    </div>
  )
}
