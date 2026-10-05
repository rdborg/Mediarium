import { useEffect, useRef, useState } from 'react'
import { api, type BookFound } from '../api'
import { useModules } from '../ModulesContext'
import { useGridColumns } from '../useGridColumns'
import { BookAddDialog, BookCard } from './BookDiscover'
import Switch from './Switch'
import { useToast } from './Toast'

// "More by <author>" on a book's page: the author's other books (the most
// read first), each with Add, and a switch to follow the author so their new
// books are added by themselves.
export default function AuthorBooks({ authorKey, author, exclude }: { authorKey: string; author: string; exclude: string }) {
  const toast = useToast()
  const { on } = useModules()
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid)
  const [works, setWorks] = useState<BookFound[] | null>(null)
  const [followed, setFollowed] = useState(false)
  const [rows, setRows] = useState(1)
  const [adding, setAdding] = useState<BookFound | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .authorWorks(authorKey)
      .then((w) => setWorks(w.filter((b) => b.key !== exclude)))
      .catch(() => setWorks([]))
    api
      .followedAuthors()
      .then((list) => setFollowed(list.some((a) => a.key === authorKey)))
      .catch(() => undefined)
  }, [authorKey, exclude])

  useEffect(() => {
    const onAdded = (e: Event) => {
      const d = (e as CustomEvent<{ key: string; id: number }>).detail
      setWorks((cur) => cur?.map((b) => (b.key === d.key ? { ...b, libraryId: d.id } : b)) ?? cur)
    }
    window.addEventListener('mediarium:book-added', onAdded)
    return () => window.removeEventListener('mediarium:book-added', onAdded)
  }, [])

  async function follow(next: boolean) {
    setBusy(true)
    try {
      if (next) {
        await api.followAuthor(authorKey, { name: author, ebook: on('ebooks'), audiobook: !on('ebooks') && on('audiobooks') })
        toast.success(`Following ${author}. Their new books are added by themselves.`)
      } else {
        await api.unfollowAuthor(authorKey)
        toast.success(`Stopped following ${author}. Their books stay in your library.`)
      }
      setFollowed(next)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (works !== null && works.length === 0) return null
  const shown = (works ?? []).slice(0, cols * rows)
  return (
    <section className="discover-rail author-books">
      <div className="rail-head">
        <h2>More by {author}</h2>
        <Switch checked={followed} onChange={(v) => void follow(v)} disabled={busy} label="Follow this author" description="New books by them are added and looked for by themselves." />
      </div>
      <div className="poster-grid" ref={grid}>
        {works === null
          ? Array.from({ length: cols }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />)
          : shown.map((b) => <BookCard key={b.key} book={b} onAdd={setAdding} />)}
      </div>
      {works && works.length > shown.length && (
        <div className="rail-foot">
          <button className="btn-sm" onClick={() => setRows((r) => r + 2)}>
            Show more
          </button>
        </div>
      )}
      <BookAddDialog target={adding} onClose={() => setAdding(null)} />
    </section>
  )
}
