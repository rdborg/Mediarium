import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import type { Book as EpubBook, NavItem, Rendition } from 'epubjs'
import { api, type Book } from '../api'
import Icon from '../components/Icon'
import { useDocumentTitle } from '../documentTitle'

type Theme = 'light' | 'sepia' | 'dark'
type Font = 'serif' | 'sans'
interface Prefs {
  size: number // percent
  theme: Theme
  font: Font
}

const PREFS_KEY = 'mediarium.reader'
const THEMES: Record<Theme, { bg: string; fg: string; link: string }> = {
  light: { bg: '#fbfaf7', fg: '#1d1c1a', link: '#1f6feb' },
  sepia: { bg: '#f4ecd8', fg: '#3b2f22', link: '#8a4b14' },
  dark: { bg: '#15171b', fg: '#d9dde3', link: '#7fb4ff' },
}
const FONTS: Record<Font, string> = {
  serif: 'Georgia, "Iowan Old Style", "Palatino Linotype", serif',
  sans: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
}

function loadPrefs(): Prefs {
  try {
    const p = JSON.parse(localStorage.getItem(PREFS_KEY) ?? '{}') as Partial<Prefs>
    return { size: Math.min(200, Math.max(70, Number(p.size) || 105)), theme: p.theme && p.theme in THEMES ? p.theme : 'light', font: p.font === 'sans' ? 'sans' : 'serif' }
  } catch {
    return { size: 105, theme: 'light', font: 'serif' }
  }
}

// The reader: one book, a page at a time. EPUBs are drawn here (epub.js);
// PDFs open in the browser's own viewer; MOBI and AZW3 can't be read in a
// browser, so those can be downloaded instead.
export default function Reader() {
  const { id } = useParams()
  const bookId = Number(id)
  const [book, setBook] = useState<Book | null>(null)
  const [error, setError] = useState('')
  useDocumentTitle(book ? `${book.title} · Reading` : 'Reading')

  useEffect(() => {
    api
      .getBook(bookId)
      .then(setBook)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [bookId])

  if (error) return <ReaderMessage title="Couldn't open this book" text={error} />
  if (!book) return <div className="bks-loading">Opening…</div>
  if (book.ebook.status !== 'downloaded') return <ReaderMessage title={book.title} text="There is no ebook for this book yet. Once Mediarium has downloaded it, it opens here." />
  const format = (book.ebook.format ?? '').toLowerCase()
  if (format === 'pdf') return <PdfReader book={book} />
  // Kindle books open as an EPUB copy Mediarium makes for the reader.
  if (KINDLE.has(format)) return <EpubReader book={book} src={`${api.bookFileUrl(book.id)}?as=epub`} />
  if (format !== 'epub') {
    return (
      <ReaderMessage
        title={book.title}
        text={`This ebook is a ${format.toUpperCase() || 'file'} file, which browsers can't show. Download it for your e-reader, or look for an EPUB version in Mediarium.`}
        action={
          <a className="primary btn-with-icon" href={api.bookFileUrl(book.id, true)}>
            <Icon name="download" size={16} /> Download
          </a>
        }
      />
    )
  }
  return <EpubReader book={book} src={api.bookFileUrl(book.id)} />
}

const KINDLE = new Set(['mobi', 'azw', 'azw3', 'prc'])

function ReaderMessage({ title, text, action }: { title: string; text: string; action?: React.ReactNode }) {
  return (
    <div className="bks-message">
      <Icon name="book" size={44} />
      <h1>{title}</h1>
      <p>{text}</p>
      <div className="row-actions">
        {action}
        <Link className="btn-with-icon" to="/bookshelf">
          <Icon name="chevron-left" size={16} /> Back to your books
        </Link>
      </div>
    </div>
  )
}

function PdfReader({ book }: { book: Book }) {
  useEffect(() => {
    // Opening it is enough to put it under Continue.
    void api.saveBookProgress(book.id, { format: 'ebook', position: 'pdf', percent: 1 })
  }, [book.id])
  return (
    <div className="rdr rdr-pdf">
      <header className="rdr-top">
        <Link className="icon-btn" to="/bookshelf" title="Back to your books" aria-label="Back to your books">
          <Icon name="chevron-left" size={20} />
        </Link>
        <strong className="rdr-title">{book.title}</strong>
        <a className="icon-btn" href={api.bookFileUrl(book.id, true)} title="Download" aria-label="Download">
          <Icon name="download" size={18} />
        </a>
      </header>
      <iframe className="rdr-pdf-frame" src={api.bookFileUrl(book.id)} title={book.title} />
    </div>
  )
}

function EpubReader({ book, src }: { book: Book; src: string }) {
  const viewer = useRef<HTMLDivElement>(null)
  const epub = useRef<EpubBook | null>(null)
  const rendition = useRef<Rendition | null>(null)
  const [prefs, setPrefs] = useState<Prefs>(loadPrefs)
  const [toc, setToc] = useState<NavItem[]>([])
  const [panel, setPanel] = useState<'' | 'toc' | 'settings'>('')
  const [percent, setPercent] = useState(0)
  const [located, setLocated] = useState(false) // page percentages are ready
  const [chapter, setChapter] = useState('')
  const [error, setError] = useState('')
  const [ready, setReady] = useState(false)
  const last = useRef<{ cfi: string; percent: number } | null>(null)
  const saveTimer = useRef<number | undefined>(undefined)
  // Nothing is saved until the book has opened where it was left: a book
  // drawn too early (a hidden window) reports the start, which must never
  // replace the real place.
  const armed = useRef(false)

  const save = useCallback(
    (keepalive = false) => {
      const at = last.current
      if (!at || !armed.current) return
      void api.saveBookProgress(book.id, { format: 'ebook', position: at.cfi, percent: at.percent, finished: at.percent >= 99.5 }, keepalive).catch(() => undefined)
    },
    [book.id],
  )

  // Open the book once.
  useEffect(() => {
    let cancelled = false
    let cleanup = () => {}
    void (async () => {
      try {
        const [{ default: ePub }, data, saved] = await Promise.all([
          import('epubjs'),
          fetch(src, { credentials: 'include' }).then(async (r) => {
            if (r.status === 422) throw new Error(((await r.json().catch(() => null)) as { error?: string } | null)?.error ?? "This book couldn't be opened here.")
            if (!r.ok) throw new Error(r.status === 404 ? "Couldn't find the book's file. It may have been moved or deleted." : `The book didn't load (error ${r.status}).`)
            return r.arrayBuffer()
          }),
          api.bookProgress(book.id, 'ebook').catch(() => null),
        ])
        if (cancelled || !viewer.current) return
        await hasRoom(viewer.current)
        if (cancelled || !viewer.current) return
        const b = ePub(data)
        epub.current = b
        const r = b.renderTo(viewer.current, { width: '100%', height: '100%', flow: 'paginated', spread: 'auto', allowScriptedContent: false })
        rendition.current = r
        for (const [name, t] of Object.entries(THEMES)) {
          r.themes.register(name, { body: { background: `${t.bg} !important`, color: `${t.fg} !important` }, a: { color: `${t.link} !important` } })
        }
        applyPrefs(r, loadPrefs())
        const tocRef = { current: [] as NavItem[] }
        r.on('relocated', (loc: { start: { cfi: string; percentage: number; href: string } }) => {
          const cfi = loc.start.cfi
          const pct = b.locations.length() > 0 ? b.locations.percentageFromCfi(cfi) * 100 : loc.start.percentage * 100
          setPercent(pct)
          last.current = { cfi, percent: Math.round(pct * 10) / 10 }
          const item = findChapter(tocRef.current, loc.start.href)
          if (item) setChapter(item.label.trim())
          window.clearTimeout(saveTimer.current)
          if (armed.current && document.visibilityState === 'visible') saveTimer.current = window.setTimeout(() => save(), 1500)
        })
        // Swipe left or right on a touch screen.
        let startX = 0
        r.on('touchstart', (e: TouchEvent) => {
          startX = e.changedTouches[0]?.screenX ?? 0
        })
        r.on('touchend', (e: TouchEvent) => {
          const dx = (e.changedTouches[0]?.screenX ?? 0) - startX
          if (Math.abs(dx) > 50) void (dx < 0 ? r.next() : r.prev())
        })
        r.on('keyup', onKey)
        b.loaded.navigation.then((nav) => {
          tocRef.current = nav.toc
          setToc(nav.toc)
        })
        const start = saved?.position && saved.position.startsWith('epubcfi') ? saved.position : undefined
        await r.display(start)
        if (cancelled) return
        setReady(true)
        window.setTimeout(() => {
          armed.current = true
        }, 1000)
        // Page percentages: worked out once per book and kept in this browser.
        const key = `mediarium.reader.loc.${book.id}`
        let cached: string | null = null
        try {
          cached = localStorage.getItem(key)
        } catch {
          cached = null
        }
        if (cached) {
          b.locations.load(cached)
        } else {
          await b.locations.generate(1600)
          try {
            localStorage.setItem(key, b.locations.save())
          } catch {
            // a full storage only means working it out again next time
          }
        }
        if (cancelled) return
        setLocated(true)
        const cur = r.currentLocation() as unknown as { start?: { cfi: string } }
        if (cur?.start?.cfi) setPercent(b.locations.percentageFromCfi(cur.start.cfi) * 100)
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      }
    })()
    function onKey(e: KeyboardEvent) {
      if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') void rendition.current?.next()
      if (e.key === 'ArrowLeft' || e.key === 'PageUp') void rendition.current?.prev()
    }
    document.addEventListener('keyup', onKey)
    const onHide = () => {
      if (document.visibilityState === 'hidden') save(true)
    }
    document.addEventListener('visibilitychange', onHide)
    window.addEventListener('pagehide', onHide)
    cleanup = () => {
      document.removeEventListener('keyup', onKey)
      document.removeEventListener('visibilitychange', onHide)
      window.removeEventListener('pagehide', onHide)
    }
    return () => {
      cancelled = true
      cleanup()
      window.clearTimeout(saveTimer.current)
      save(true)
      epub.current?.destroy()
      epub.current = null
      rendition.current = null
    }
  }, [book.id, save, src])

  useEffect(() => {
    try {
      localStorage.setItem(PREFS_KEY, JSON.stringify(prefs))
    } catch {
      // the reader still works; the choice just isn't remembered
    }
    if (rendition.current) applyPrefs(rendition.current, prefs)
  }, [prefs])

  const theme = THEMES[prefs.theme]
  if (error)
    return (
      <ReaderMessage
        title={book.title}
        text={error}
        action={
          <a className="primary btn-with-icon" href={api.bookFileUrl(book.id, true)}>
            <Icon name="download" size={16} /> Download
          </a>
        }
      />
    )
  return (
    <div className={`rdr theme-${prefs.theme}`} style={{ ['--rdr-bg' as string]: theme.bg, ['--rdr-fg' as string]: theme.fg }}>
      <header className="rdr-top">
        <Link className="icon-btn" to="/bookshelf" title="Back to your books" aria-label="Back to your books">
          <Icon name="chevron-left" size={20} />
        </Link>
        <span className="rdr-title">
          <strong>{book.title}</strong>
          {chapter && <small>{chapter}</small>}
        </span>
        <button className={`icon-btn${panel === 'toc' ? ' active' : ''}`} onClick={() => setPanel(panel === 'toc' ? '' : 'toc')} title="Contents" aria-label="Contents">
          <Icon name="list" size={18} />
        </button>
        <button className={`icon-btn${panel === 'settings' ? ' active' : ''}`} onClick={() => setPanel(panel === 'settings' ? '' : 'settings')} title="Text and colours" aria-label="Text and colours">
          <span className="rdr-aa">Aa</span>
        </button>
        <button
          className="icon-btn"
          onClick={() => (document.fullscreenElement ? void document.exitFullscreen() : void document.documentElement.requestFullscreen?.())}
          title="Full screen"
          aria-label="Full screen"
        >
          <Icon name="monitor" size={18} />
        </button>
      </header>

      {panel === 'toc' && (
        <nav className="rdr-panel rdr-toc" aria-label="Contents">
          {toc.length === 0 && <p className="hint">This book has no table of contents.</p>}
          <TocList
            items={toc}
            onPick={(href) => {
              void rendition.current?.display(href)
              setPanel('')
            }}
          />
        </nav>
      )}
      {panel === 'settings' && (
        <div className="rdr-panel rdr-settings">
          <div className="rdr-row">
            <span>Text size</span>
            <button className="btn-sm" onClick={() => setPrefs((p) => ({ ...p, size: Math.max(70, p.size - 10) }))} aria-label="Smaller text">
              A−
            </button>
            <span className="rdr-size">{prefs.size}%</span>
            <button className="btn-sm" onClick={() => setPrefs((p) => ({ ...p, size: Math.min(200, p.size + 10) }))} aria-label="Bigger text">
              A+
            </button>
          </div>
          <div className="rdr-row">
            <span>Page</span>
            {(Object.keys(THEMES) as Theme[]).map((t) => (
              <button key={t} className={`rdr-swatch${prefs.theme === t ? ' active' : ''}`} style={{ background: THEMES[t].bg, color: THEMES[t].fg }} onClick={() => setPrefs((p) => ({ ...p, theme: t }))}>
                {t === 'light' ? 'Light' : t === 'sepia' ? 'Sepia' : 'Dark'}
              </button>
            ))}
          </div>
          <div className="rdr-row">
            <span>Font</span>
            {(['serif', 'sans'] as Font[]).map((f) => (
              <button key={f} className={`btn-sm${prefs.font === f ? ' primary' : ''}`} style={{ fontFamily: FONTS[f] }} onClick={() => setPrefs((p) => ({ ...p, font: f }))}>
                {f === 'serif' ? 'Book' : 'Plain'}
              </button>
            ))}
          </div>
        </div>
      )}

      <main className="rdr-page">
        <button className="rdr-turn prev" onClick={() => void rendition.current?.prev()} aria-label="Previous page">
          <Icon name="chevron-left" size={26} />
        </button>
        <div className="rdr-viewer" ref={viewer} />
        <button className="rdr-turn next" onClick={() => void rendition.current?.next()} aria-label="Next page">
          <Icon name="chevron-right" size={26} />
        </button>
        {!ready && <div className="rdr-opening">Opening the book…</div>}
      </main>

      <footer className="rdr-foot">
        <input
          type="range"
          min={0}
          max={100}
          step={0.1}
          value={percent}
          disabled={!located}
          title={located ? 'Go to a place in the book' : 'Working out the pages…'}
          aria-label="Place in the book"
          onChange={(e) => setPercent(Number(e.target.value))}
          onMouseUp={(e) => jump(Number((e.target as HTMLInputElement).value))}
          onTouchEnd={(e) => jump(Number((e.target as HTMLInputElement).value))}
          onKeyUp={(e) => jump(Number((e.target as HTMLInputElement).value))}
        />
        <span className="rdr-pct">{located ? `${Math.round(percent)}%` : '…'}</span>
      </footer>
    </div>
  )

  function jump(p: number) {
    const b = epub.current
    if (!b || b.locations.length() === 0) return
    void rendition.current?.display(b.locations.cfiFromPercentage(p / 100))
  }
}

// hasRoom waits until el has a size: a book laid out in a hidden window has
// no pages to turn.
function hasRoom(el: HTMLElement): Promise<void> {
  if (el.offsetWidth > 0 && el.offsetHeight > 0) return Promise.resolve()
  return new Promise((resolve) => {
    const ro = new ResizeObserver(() => {
      if (el.offsetWidth > 0 && el.offsetHeight > 0) {
        ro.disconnect()
        resolve()
      }
    })
    ro.observe(el)
  })
}

function applyPrefs(r: Rendition, p: Prefs) {
  r.themes.select(p.theme)
  r.themes.fontSize(`${p.size}%`)
  r.themes.font(FONTS[p.font])
}

function findChapter(items: NavItem[], href: string): NavItem | undefined {
  const base = href.split('#')[0]
  for (const it of items) {
    if (it.href.split('#')[0] === base) return it
    const sub = findChapter(it.subitems ?? [], href)
    if (sub) return sub
  }
  return undefined
}

function TocList({ items, onPick }: { items: NavItem[]; onPick: (href: string) => void }) {
  return (
    <ul>
      {items.map((it) => (
        <li key={it.id || it.href}>
          <button onClick={() => onPick(it.href)}>{it.label.trim()}</button>
          {it.subitems && it.subitems.length > 0 && <TocList items={it.subitems} onPick={onPick} />}
        </li>
      ))}
    </ul>
  )
}
