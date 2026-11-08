import { useCallback, useEffect, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { openFromList, TWO_PANE_QUERY } from './twoPane'

// True while the window is tablet-sized (see TWO_PANE_QUERY), and follows
// rotating the tablet, split view and resizing the window.
export function useTwoPaneWidth(): boolean {
  const [on, setOn] = useState(() => typeof window.matchMedia === 'function' && window.matchMedia(TWO_PANE_QUERY).matches)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia(TWO_PANE_QUERY)
    const change = () => setOn(mq.matches)
    change()
    mq.addEventListener('change', change)
    return () => mq.removeEventListener('change', change)
  }, [])
  return on
}

// The address for opening an item from a Library list. On a tablet it carries the
// list's own settings and the mark that keeps the list beside the item; anywhere
// else it is the plain address.
export function useOpenFromList(): (path: string) => string {
  const wide = useTwoPaneWidth()
  const { search } = useLocation()
  return useCallback((path: string) => (wide ? openFromList(path, search) : path), [wide, search])
}
