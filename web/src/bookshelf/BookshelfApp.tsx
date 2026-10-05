import { lazy, Suspense, useEffect } from 'react'
import { Route, Routes } from 'react-router-dom'
import Shelf from './Shelf'
import './bookshelf.css'

// The reader brings in the EPUB library, so it loads only when a book is opened.
const Reader = lazy(() => import('./Reader'))
const Player = lazy(() => import('./Player'))

// Mediarium Books: reading and listening, as an app of its own. It has no
// Mediarium sidebar, its own name and icon when installed (its own web app
// manifest, scoped to /bookshelf/), and opens in a window of its own.
export default function BookshelfApp() {
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="manifest"]')
    const before = link?.getAttribute('href') ?? null
    link?.setAttribute('href', '/bookshelf.webmanifest')
    document.documentElement.classList.add('bookshelf-mode')
    return () => {
      if (link && before) link.setAttribute('href', before)
      document.documentElement.classList.remove('bookshelf-mode')
    }
  }, [])

  return (
    <div className="bks">
      <Suspense fallback={<div className="bks-loading">Opening…</div>}>
        <Routes>
          <Route index element={<Shelf />} />
          <Route path="read/:id" element={<Reader />} />
          <Route path="listen/:id" element={<Player />} />
          <Route path="*" element={<Shelf />} />
        </Routes>
      </Suspense>
    </div>
  )
}
