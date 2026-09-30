import { useEffect, useRef } from 'react'

// Brings a form that has just opened into view and puts the cursor in its
// first field, so pressing Edit never leaves the form off-screen.
//
//   const [opened, setOpened] = useState(0)          // bump when a form opens
//   const formRef = useReveal<HTMLDivElement>(opened)
//   <div ref={formRef}>...</div>
//
// `opened` is a counter: every change (other than back to 0) reveals the form
// again, even when the same profile is edited twice in a row.
export function useReveal<T extends HTMLElement>(opened: number) {
  const ref = useRef<T>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || opened === 0) return
    const calm = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
    el.scrollIntoView({ behavior: calm ? 'auto' : 'smooth', block: 'start' })
    el.querySelector<HTMLElement>('input:not([type="hidden"]):not([disabled]), select:not([disabled]), textarea:not([disabled])')?.focus({ preventScroll: true })
  }, [opened])
  return ref
}
