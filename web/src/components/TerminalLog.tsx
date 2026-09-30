import { useEffect, useRef, useState } from 'react'
import { copyText } from './CopyBox'
import Icon from './Icon'
import './TerminalLog.css'

export interface LogLine {
  time: string // an ISO time
  text: string
  ok: boolean
}

function clock(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n: number, w = 2) => String(n).padStart(w, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
}

// A dark, monospace log: one timestamped line per step, newest at the bottom,
// scrolled into view as lines arrive. `running` adds a waiting line with a
// blinking cursor. Lines are revealed one after another so it reads as it
// happened, unless the person prefers less motion.
export default function TerminalLog({ lines, running = false, label = 'Log' }: { lines: LogLine[]; running?: boolean; label?: string }) {
  const [shown, setShown] = useState(lines.length)
  const [copied, setCopied] = useState(false)
  const body = useRef<HTMLDivElement>(null)
  const seen = useRef(lines)

  useEffect(() => {
    const calm = typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    if (calm || lines.length <= 1 || seen.current === lines) {
      seen.current = lines
      setShown(lines.length)
      return
    }
    seen.current = lines
    setShown(0)
    const step = Math.max(35, Math.min(140, 1400 / lines.length))
    let n = 0
    const id = window.setInterval(() => {
      n += 1
      setShown(n)
      if (n >= lines.length) window.clearInterval(id)
    }, step)
    return () => window.clearInterval(id)
  }, [lines])

  useEffect(() => {
    const el = body.current
    if (el) el.scrollTop = el.scrollHeight
  }, [shown, running, lines])

  const text = lines.map((l) => `[${clock(l.time)}] ${l.ok ? '' : 'FAILED: '}${l.text}`).join('\n')

  async function copy() {
    if (await copyText(text)) {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    }
  }

  return (
    <div className="term" role="log" aria-label={label} aria-live="polite">
      <div className="term-bar">
        <span className="term-title">{label}</span>
        {lines.length > 0 && (
          <button type="button" className="term-copy" onClick={() => void copy()} aria-label="Copy the log">
            {copied ? <Icon name="check" size={13} /> : null} {copied ? 'Copied' : 'Copy'}
          </button>
        )}
      </div>
      <div className="term-body" ref={body} tabIndex={0}>
        {lines.slice(0, shown).map((l, i) => (
          <div key={i} className={`term-line${l.ok ? '' : ' bad'}`}>
            <span className="term-time">{clock(l.time)}</span>
            <span className="term-mark" aria-hidden="true">{l.ok ? '>' : '!'}</span>
            <span className="term-text">{l.text}</span>
          </div>
        ))}
        {running && (
          <div className="term-line wait">
            <span className="term-time" />
            <span className="term-mark" aria-hidden="true">&gt;</span>
            <span className="term-text">Working<span className="term-cursor" /></span>
          </div>
        )}
      </div>
    </div>
  )
}
