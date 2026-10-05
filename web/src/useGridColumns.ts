import { useEffect, useState, type RefObject } from 'react'

// How many columns a CSS grid currently shows, updated as the window (or the
// sidebar) changes size. Used to fill rows completely instead of leaving a
// ragged last row.
export function useGridColumns(ref: RefObject<HTMLElement | null>, fallback = 6): number {
  const [cols, setCols] = useState(fallback)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    // Only real track sizes count: a grid that isn't laid out yet answers
    // "none", which is not one column.
    const measure = () => {
      const n = getComputedStyle(el).gridTemplateColumns.split(' ').filter((v) => v.endsWith('px')).length
      if (n > 0) setCols(n)
    }
    measure()
    // A page that is still settling (fonts, an entrance animation) can give
    // the first answer too early, without a resize to correct it.
    const frame = requestAnimationFrame(measure)
    const later = window.setTimeout(measure, 400)
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => {
      cancelAnimationFrame(frame)
      window.clearTimeout(later)
      ro.disconnect()
    }
  }, [ref])
  return cols
}
