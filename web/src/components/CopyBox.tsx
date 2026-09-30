import { useEffect, useRef, useState } from 'react'
import Icon from './Icon'

// Copies text. The clipboard API only exists on secure pages (https or
// localhost), and a NAS is usually opened over plain http, so fall back to
// the old way of copying from a hidden box.
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to the old way
  }
  try {
    const box = document.createElement('textarea')
    box.value = text
    box.setAttribute('readonly', '')
    box.style.position = 'fixed'
    box.style.opacity = '0'
    document.body.appendChild(box)
    box.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(box)
    return ok
  } catch {
    return false
  }
}

// A line of text (a compose line, for example) with a Copy button next to it.
export default function CopyBox({ text, label }: { text: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  async function copy() {
    setState((await copyText(text)) ? 'copied' : 'failed')
    clearTimeout(timer.current)
    timer.current = setTimeout(() => setState('idle'), 2200)
  }

  return (
    <div className="copy-box">
      <code aria-label={label}>{text}</code>
      <button type="button" className="btn-sm btn-with-icon" onClick={() => void copy()}>
        {state === 'copied' && <Icon name="check" size={14} />}
        {state === 'copied' ? 'Copied' : state === 'failed' ? 'Select and copy' : 'Copy'}
      </button>
    </div>
  )
}
