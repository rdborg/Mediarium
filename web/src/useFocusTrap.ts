import { useEffect, type RefObject } from 'react'

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

// Keeps the keyboard inside an open dialog: focus moves into it when it
// opens, Tab and Shift+Tab go round its controls instead of out to the page
// behind, and focus goes back to where it was when the dialog closes.
// Put the returned-to ref on the dialog box (give it tabIndex={-1}).
export function useFocusTrap(ref: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const box = ref.current
    if (!box) return
    const before = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!box.contains(document.activeElement)) {
      // A form control is a better place to start than the close button.
      const first = box.querySelector<HTMLElement>('.modal-body select, .modal-body input, .modal-body textarea') ?? box.querySelector<HTMLElement>(FOCUSABLE) ?? box
      first.focus({ preventScroll: true })
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return
      const items = [...box.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.getClientRects().length > 0)
      if (items.length === 0) {
        e.preventDefault()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const at = document.activeElement
      if (e.shiftKey && (at === first || !box.contains(at))) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (at === last || !box.contains(at))) {
        e.preventDefault()
        first.focus()
      }
    }
    box.addEventListener('keydown', onKey)
    return () => {
      box.removeEventListener('keydown', onKey)
      if (before?.isConnected) before.focus({ preventScroll: true })
    }
  }, [ref])
}
