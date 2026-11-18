import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { helpPath } from '../guide/path.ts'
import { guideClick } from '../guide/popup.ts'
import { t } from '../i18n'
import './InfoTip.css'

// A small round button that explains the thing next to it in one sentence.
//
//   <TipLabel text={sentence}>{t('Search now')}</TipLabel>
//   <TipLabel help="quality#automatic-searching">{t('Automation')}</TipLabel>
//   <Switch label={t('Name')} tip={sentence} learnMore="quality#automatic-searching" />
//   <InfoTip text={sentence} learnMore="quality#automatic-searching" />
//
// - `text` is one plain sentence: what the setting does and what happens when
//   you change it. Write it with t() like any other text. Put it after a label
//   with TipLabel (or the tip prop of Switch): they keep the last word and the
//   button together, so the button never wraps onto a line of its own. Line
//   breaks (a backslash and n in the text) are kept, for a short list of meanings.
// - `learnMore` ("topic" or "topic#anchor" of the user guide) adds a "Learn more"
//   link to the popover. HelpLink is the same thing with a question mark.
// - `label` is the button's name for screen readers (default "More information").
//
// It opens on hover, on keyboard focus and on a click or tap (the click keeps it
// open until you press it again, press Escape or click elsewhere). It is drawn
// on top of the page in a portal, so a scrolling box or a card with
// overflow:hidden never cuts it off. The sentence is also given to screen readers
// as the button's description at all times.
export interface InfoTipProps {
  text: string
  label?: string
  learnMore?: string
  glyph?: 'i' | '?'
  className?: string
}

const GAP = 8
const EDGE = 8

export default function InfoTip({ text, label, learnMore, glyph = 'i', className = '' }: InfoTipProps) {
  const descId = useId()
  const popId = useId()
  const button = useRef<HTMLButtonElement>(null)
  const pop = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [at, setAt] = useState<{ left: number; top: number; below: boolean } | null>(null)
  const openTimer = useRef(0)
  const closeTimer = useRef(0)

  const clearTimers = () => {
    window.clearTimeout(openTimer.current)
    window.clearTimeout(closeTimer.current)
  }
  const close = useCallback(() => {
    window.clearTimeout(openTimer.current)
    window.clearTimeout(closeTimer.current)
    setOpen(false)
    setPinned(false)
  }, [])
  useEffect(() => clearTimers, [])

  // Inside a <label> the tip's own button comes first, so a screen reader would take the button, not the box
  // after it, as the thing the label names. Point the label at that box.
  useEffect(() => {
    const label = button.current?.closest('label')
    if (!label || label.htmlFor || label.querySelector('input, select, textarea') === null) return
    const control = label.querySelector<HTMLElement>('input:not([type="hidden"]), select, textarea')
    if (!control) return
    if (!control.id) control.id = `${descId}-field`
    label.htmlFor = control.id
  }, [descId])

  const place = useCallback(() => {
    const b = button.current
    const p = pop.current
    if (!b || !p) return
    const r = b.getBoundingClientRect()
    const w = p.offsetWidth
    const h = p.offsetHeight
    const roomAbove = r.top - GAP - EDGE
    const below = roomAbove < h && window.innerHeight - r.bottom - GAP - EDGE > roomAbove
    const left = Math.min(Math.max(r.left + r.width / 2 - w / 2, EDGE), Math.max(EDGE, window.innerWidth - w - EDGE))
    const top = below ? r.bottom + GAP : r.top - h - GAP
    setAt({ left, top, below })
  }, [])

  // Measured after it is drawn, then kept in place while the page moves.
  useLayoutEffect(() => {
    if (!open) return
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open, place])

  // Escape and a press anywhere else close it.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      // A window the tip sits in must stay open: this Escape only closes the tip.
      e.stopPropagation()
      const inside = pop.current?.contains(document.activeElement)
      close()
      if (inside) button.current?.focus()
    }
    const onDown = (e: Event) => {
      const target = e.target as Node
      if (button.current?.contains(target) || pop.current?.contains(target)) return
      close()
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onDown, true)
    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onDown, true)
    }
  }, [open, close])

  const hover = (e: React.PointerEvent) => e.pointerType !== 'touch'
  const enter = (e: React.PointerEvent) => {
    if (!hover(e)) return
    clearTimers()
    if (open) return
    openTimer.current = window.setTimeout(() => setOpen(true), 120)
  }
  const leave = (e: React.PointerEvent) => {
    if (!hover(e) || pinned) return
    clearTimers()
    closeTimer.current = window.setTimeout(close, 140)
  }

  return (
    <>
      <button
        ref={button}
        type="button"
        className={`info-tip${open ? ' open' : ''}${glyph === '?' ? ' help' : ''}${className ? ` ${className}` : ''}`}
        aria-label={label ?? (learnMore ? t('Help') : t('More information'))}
        aria-describedby={descId}
        aria-expanded={open}
        aria-controls={open ? popId : undefined}
        onPointerEnter={enter}
        onPointerLeave={leave}
        onFocus={(e) => {
          // Only keyboard focus opens it; a mouse click is handled by onClick.
          if (e.currentTarget.matches(':focus-visible')) {
            clearTimers()
            setOpen(true)
          }
        }}
        onBlur={(e) => {
          if (pinned || (e.relatedTarget instanceof Node && pop.current?.contains(e.relatedTarget))) return
          close()
        }}
        onClick={(e) => {
          // It sits inside labels, rows and links, so the press must stay here.
          e.preventDefault()
          e.stopPropagation()
          clearTimers()
          if (open && pinned) return close()
          setOpen(true)
          setPinned(true)
          // From the keyboard, move to the link so Enter can follow it.
          if (learnMore && e.detail === 0) requestAnimationFrame(() => pop.current?.querySelector('a')?.focus())
        }}
      >
        <span aria-hidden="true">{glyph === '?' ? '?' : 'i'}</span>
      </button>
      <span id={descId} className="sr-only" aria-hidden="true">
        {text}
      </span>
      {open &&
        createPortal(
          <div
            ref={pop}
            id={popId}
            className={`info-pop${at?.below ? ' below' : ''}`}
            role={learnMore ? 'group' : 'tooltip'}
            aria-label={learnMore ? (label ?? t('Help')) : undefined}
            style={{ left: at?.left ?? 0, top: at?.top ?? 0, visibility: at ? 'visible' : 'hidden' }}
            onPointerEnter={() => clearTimers()}
            onPointerLeave={(e) => {
              if (e.pointerType === 'touch' || pinned) return
              closeTimer.current = window.setTimeout(close, 140)
            }}
          >
            <p>{text}</p>
            {learnMore && (
              <Link to={helpPath(learnMore)} onClick={guideClick(learnMore, close)}>
                {t('Learn more')}
              </Link>
            )}
          </div>,
          document.body,
        )}
    </>
  )
}
