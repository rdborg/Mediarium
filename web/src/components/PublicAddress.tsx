import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'
import { url as urlCheck } from '../validate'

// The address you open Mediarium at. Messages use it for their "Open in
// Mediarium" button; with none set they carry no link. Saves by itself a
// moment after you stop typing, or when you leave the box.
export default function PublicAddress() {
  const toast = useToast()
  const [stored, setStored] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const timer = useRef<number | undefined>(undefined)

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setStored(s.publicUrl ?? '')
        setText(s.publicUrl ?? '')
      })
      .catch(() => undefined)
    return () => window.clearTimeout(timer.current)
  }, [])

  const error = text.trim() === '' ? null : urlCheck(text.trim(), { example: 'https://mediarium.example.com' })

  async function save(value: string) {
    window.clearTimeout(timer.current)
    const v = value.trim().replace(/\/+$/, '')
    if (stored === null || error || v === stored) return
    setBusy(true)
    try {
      const s = await api.putSettings({ publicUrl: v })
      setStored(s.publicUrl ?? '')
      setText(s.publicUrl ?? '')
      toast.success(v ? 'Saved: messages now link to Mediarium.' : 'Saved: messages carry no link.')
    } catch (e) {
      toast.error(`Not saved: ${e instanceof Error ? e.message : String(e)}`)
      setText(stored)
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group folders span-all">
      <legend>
        <Icon name="external" size={14} /> Link in messages
      </legend>
      <label className="field" htmlFor="public-address" style={{ display: 'grid', gap: 6, maxWidth: 480 }}>
        <span>Address of Mediarium</span>
        <input
          id="public-address"
          value={text}
          placeholder="https://mediarium.example.com"
          disabled={stored === null || busy}
          onChange={(e) => {
            setText(e.target.value)
            window.clearTimeout(timer.current)
            timer.current = window.setTimeout(() => void save(e.target.value), 1200)
          }}
          onBlur={(e) => void save(e.target.value)}
          aria-invalid={error ? true : undefined}
        />
      </label>
      <small style={{ color: error ? 'var(--danger)' : 'var(--text-dim)' }}>{error ?? 'Emails and push messages get an Open in Mediarium button that goes here. Leave it empty for no link.'}</small>
    </fieldset>
  )
}
