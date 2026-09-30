import { useEffect, useState, type RefObject } from 'react'

// How many columns a CSS grid currently shows, updated as the window (or the
// sidebar) changes size. Used to fill rows completely instead of leaving a
// ragged last row.
export function useGridColumns(ref: RefObject<HTMLElement | null>, fallback = 6): number {
  const [cols, setCols] = useState(fallback)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => {
      const n = getComputedStyle(el).gridTemplateColumns.split(' ').filter(Boolean).length
      if (n > 0) setCols(n)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [ref])
  return cols
}
