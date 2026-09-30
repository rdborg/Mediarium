import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import Icon from './Icon'

export interface DropdownOption {
  value: string
  label: string
}

interface Props {
  value: string
  options: DropdownOption[]
  onChange: (value: string) => void
  label: string // accessible name
  disabled?: boolean
  title?: string
}

// A select in the app's own style whose list always opens below the field
// (native menus open on top of it on macOS and look different everywhere).
// Keyboard: arrows move, Enter or Space picks, Escape closes, typing a letter
// jumps to the next option starting with it.
export default function Dropdown({ value, options, onChange, label, disabled, title }: Props) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const root = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLUListElement>(null)
  const id = useId()
  const current = options.find((o) => o.value === value) ?? options[0]

  // Callers pass a fresh options array every render, so opening only
  // depends on `open`; the current values are read through a ref.
  const latest = useRef({ options, value })
  latest.current = { options, value }
  useEffect(() => {
    if (!open) return
    const { options: opts, value: v } = latest.current
    setActive(Math.max(0, opts.findIndex((o) => o.value === v)))
    const onDoc = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  useEffect(() => {
    if (open) list.current?.children[active]?.scrollIntoView({ block: 'nearest' })
  }, [open, active])

  function pick(i: number) {
    const o = options[i]
    if (o) onChange(o.value)
    setOpen(false)
  }

  function onKey(e: KeyboardEvent) {
    if (disabled) return
    if (!open && (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault()
      setOpen(true)
      return
    }
    if (!open) return
    if (e.key === 'Escape') setOpen(false)
    else if (e.key === 'ArrowDown') setActive((a) => Math.min(options.length - 1, a + 1))
    else if (e.key === 'ArrowUp') setActive((a) => Math.max(0, a - 1))
    else if (e.key === 'Home') setActive(0)
    else if (e.key === 'End') setActive(options.length - 1)
    else if (e.key === 'Enter' || e.key === ' ') pick(active)
    else if (e.key === 'Tab') setOpen(false)
    else if (e.key.length === 1) {
      const k = e.key.toLowerCase()
      const next = options.findIndex((o, i) => i > active && o.label.toLowerCase().startsWith(k))
      const first = options.findIndex((o) => o.label.toLowerCase().startsWith(k))
      if (next >= 0 || first >= 0) setActive(next >= 0 ? next : first)
      return
    } else return
    e.preventDefault()
  }

  return (
    <div className={`dropdown${open ? ' open' : ''}`} ref={root}>
      <button
        type="button"
        className="dropdown-btn"
        onClick={() => !disabled && setOpen((o) => !o)}
        onKeyDown={onKey}
        disabled={disabled}
        title={title}
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={id}
      >
        <span>{current?.label}</span>
        <Icon name="chevron-down" size={15} />
      </button>
      {open && (
        <ul className="dropdown-list" role="listbox" id={id} ref={list} aria-label={label}>
          {options.map((o, i) => (
            <li
              key={o.value}
              role="option"
              aria-selected={o.value === value}
              className={`${i === active ? 'active' : ''}${o.value === value ? ' selected' : ''}`}
              onMouseEnter={() => setActive(i)}
              onMouseDown={(e) => {
                e.preventDefault()
                pick(i)
              }}
            >
              {o.label}
              {o.value === value && <Icon name="check" size={14} />}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
