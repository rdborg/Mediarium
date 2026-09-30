import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import Icon, { type IconName } from './Icon'

export interface MenuChoice {
  id: string
  label: string
  hint?: string // a few words under the label
  danger?: boolean
}

interface Props {
  label: string // what the button says, and its accessible name
  icon?: IconName
  choices: MenuChoice[]
  onPick: (id: string) => void
  disabled?: boolean
  title?: string
}

// A button that opens a short list of things to do, in the same style as the
// filter dropdowns. Unlike a select it has no current value: picking a choice
// does it at once. Keyboard: arrows move, Enter or Space picks, Escape closes.
export default function ActionMenu({ label, icon, choices, onPick, disabled, title }: Props) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const [alignRight, setAlignRight] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const id = useId()

  function openMenu() {
    setActive(0)
    // Near the right edge the list opens towards the left, so it stays on screen.
    const box = root.current?.getBoundingClientRect()
    setAlignRight(!!box && box.left + 260 > window.innerWidth)
    setOpen(true)
  }

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  function pick(i: number) {
    const c = choices[i]
    setOpen(false)
    if (c) onPick(c.id)
  }

  function onKey(e: KeyboardEvent) {
    if (disabled) return
    if (!open && (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault()
      openMenu()
      return
    }
    if (!open) return
    if (e.key === 'Escape') setOpen(false)
    else if (e.key === 'ArrowDown') setActive((a) => Math.min(choices.length - 1, a + 1))
    else if (e.key === 'ArrowUp') setActive((a) => Math.max(0, a - 1))
    else if (e.key === 'Home') setActive(0)
    else if (e.key === 'End') setActive(choices.length - 1)
    else if (e.key === 'Enter' || e.key === ' ') pick(active)
    else if (e.key === 'Tab') setOpen(false)
    else return
    e.preventDefault()
  }

  return (
    <div className={`dropdown action-menu${open ? ' open' : ''}`} ref={root}>
      <button
        type="button"
        className="dropdown-btn"
        onClick={() => !disabled && (open ? setOpen(false) : openMenu())}
        onKeyDown={onKey}
        disabled={disabled}
        title={title}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={id}
      >
        {icon && <Icon name={icon} size={16} />}
        <span>{label}</span>
        <Icon name="chevron-down" size={15} />
      </button>
      {open && (
        <ul className={`dropdown-list action-menu-list${alignRight ? ' align-right' : ''}`} role="menu" id={id} aria-label={label}>
          {choices.map((c, i) => (
            <li
              key={c.id}
              role="menuitem"
              className={`${i === active ? 'active' : ''}${c.danger ? ' danger' : ''}`}
              onMouseEnter={() => setActive(i)}
              onMouseDown={(e) => {
                e.preventDefault()
                pick(i)
              }}
            >
              <span>
                {c.label}
                {c.hint && <small>{c.hint}</small>}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
