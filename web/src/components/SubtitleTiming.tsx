import { useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'

// Under a subtitle file in the Files panel: move it earlier or later, line it
// up with another subtitle of the same title that is in time, or put the
// original back.
export default function SubtitleTiming({ kind, id, file, others, onDone }: { kind: 'movie' | 'series'; id: number; file: string; others: string[]; onDone: () => void }) {
  const toast = useToast()
  const [seconds, setSeconds] = useState('1.0')
  const [ref, setRef] = useState(others[0] ?? '')
  const [busy, setBusy] = useState(false)

  async function run(body: { shiftMs?: number; reference?: string; undo?: boolean }) {
    setBusy(true)
    try {
      const r = await api.subtitleTiming({ kind, id, file, ...body })
      toast.success(r.message)
      onDone()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const secs = Number(seconds.replace(',', '.'))
  const valid = Number.isFinite(secs) && secs !== 0 && Math.abs(secs) <= 3600
  return (
    <div className="sub-timing">
      <div className="sub-timing-row">
        <span>Move by</span>
        <input type="number" step={0.1} value={seconds} onChange={(e) => setSeconds(e.target.value)} aria-label="Seconds" disabled={busy} />
        <span>seconds</span>
        <button className="btn-sm" disabled={busy || !valid} onClick={() => void run({ shiftMs: Math.round(-Math.abs(secs) * 1000) })} title="Show the lines earlier">
          Earlier
        </button>
        <button className="btn-sm" disabled={busy || !valid} onClick={() => void run({ shiftMs: Math.round(Math.abs(secs) * 1000) })} title="Show the lines later">
          Later
        </button>
      </div>
      {others.length > 0 && (
        <div className="sub-timing-row">
          <span>Line up with</span>
          <select value={ref} onChange={(e) => setRef(e.target.value)} disabled={busy} aria-label="Subtitle that is in time">
            {others.map((o) => (
              <option key={o} value={o}>
                {o.split('/').pop()}
              </option>
            ))}
          </select>
          <button className="btn-sm btn-with-icon" disabled={busy || !ref} onClick={() => void run({ reference: ref })}>
            <Icon name="refresh" size={13} /> Line up
          </button>
        </div>
      )}
      <div className="sub-timing-row">
        <button className="btn-sm" disabled={busy} onClick={() => void run({ undo: true })}>
          Put the original back
        </button>
        <small className="hint">The original is kept as a .bak file next to it.</small>
      </div>
    </div>
  )
}
