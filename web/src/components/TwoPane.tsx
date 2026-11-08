import { Suspense, useEffect, type ReactNode } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { closePaneTarget, escapeClosesPane, paneSelected } from '../twoPane'
import { useTwoPaneWidth } from '../useTwoPane'
import PageLoading from './PageLoading'

// The page for the Library and for the pages opened from it. On a phone and on a
// computer it changes nothing: the list is one page and a title is another. On a
// tablet, while a title, show, book or artist is open, the Library list stays on
// the left and the open page fills the right, so picking another one only swaps the
// right side. Both sides are ordinary pages: `list` is the Library and the right
// side is whichever route is open (the Outlet), so nothing is written twice.
export default function TwoPane({ list }: { list: (pane: boolean) => ReactNode }) {
  const loc = useLocation()
  const wide = useTwoPaneWidth()
  const pane = wide && paneSelected(loc.pathname, loc.search)
  const onLibrary = loc.pathname === '/library' || loc.pathname === '/library/'

  if (!pane) return onLibrary ? <>{list(false)}</> : <Outlet />
  return (
    <div className="twopane">
      <div className="twopane-list">{list(true)}</div>
      <div className="twopane-detail" key={loc.pathname} tabIndex={-1} data-pane-detail>
        <Suspense fallback={<PageLoading />}>
          <Outlet />
        </Suspense>
      </div>
      <EscapeCloses />
    </div>
  )
}

// Esc closes the right side and shows the list on its own again. It leaves Esc alone when it
// is pressed in a text box or while a dialog, sheet or menu is open.
function EscapeCloses() {
  const loc = useLocation()
  const navigate = useNavigate()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target instanceof HTMLElement ? e.target : null
      const inField = !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
      const overlayOpen = !!document.querySelector('[role="dialog"], [role="menu"], [role="listbox"], .modal-backdrop:not([hidden]), .sidebar.open')
      if (!escapeClosesPane({ key: e.key, defaultPrevented: e.defaultPrevented, inField, overlayOpen })) return
      navigate(closePaneTarget(loc.pathname, loc.search))
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [loc.pathname, loc.search, navigate])
  return null
}
