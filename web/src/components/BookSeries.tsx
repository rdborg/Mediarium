import { useEffect, useRef, useState } from 'react'
import { api, type BookFound, type BookSeries as Series, type BookSeriesEntry } from '../api'
import { entryMeta } from '../bookSeries'
import { useModules } from '../ModulesContext'
import { useGridColumns } from '../useGridColumns'
import { BookAddDialog, BookCard } from './BookDiscover'
import Switch from './Switch'
import { useToast } from './Toast'

// "Book 2 of <series>" on a book's page: every book of the series in reading
// order, each with Add, and a switch to follow the series so its missing books
// are added now and new ones as they come out.

function asFound(e: BookSeriesEntry): BookFound {
  return { key: e.key ?? '', title: e.title, author: e.author, authorKey: e.authorKey, year: e.year, coverUrl: e.coverUrl, libraryId: e.libraryId, hasEbook: e.hasEbook, hasAudio: e.hasAudio }
}

export default function BookSeries({ workKey, title, author }: { workKey: string; title: string; author: string }) {
  const toast = useToast()
  const { on } = useModules()
  const grid = useRef<HTMLDivElement>(null)
  const cols = useGridColumns(grid)
  const [series, setSeries] = useState<Series | null | undefined>(undefined)
  const [rows, setRows] = useState(1)
  const [adding, setAdding] = useState<BookFound | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setSeries(undefined)
    api
      .bookSeries(workKey, title, author)
      .then((r) => setSeries(r.series))
      .catch(() => setSeries(null))
  }, [workKey, title, author])

  useEffect(() => {
    const onAdded = (e: Event) => {
      const d = (e as CustomEvent<{ key: string; id: number }>).detail
      setSeries((cur) => (cur ? { ...cur, entries: cur.entries.map((b) => (b.key === d.key ? { ...b, libraryId: d.id } : b)) } : cur))
    }
    window.addEventListener('mediarium:book-added', onAdded)
    return () => window.removeEventListener('mediarium:book-added', onAdded)
  }, [])

  async function follow(next: boolean) {
    if (!series) return
    setBusy(true)
    try {
      if (next) {
        const ebook = on('ebooks')
        const res = await api.followSeries(series.source, series.key, { name: series.name, ebook, audiobook: !ebook && on('audiobooks') })
        toast.success(
          res.message ??
            (res.added > 0
              ? `Following ${series.name}. ${res.added === 1 ? 'One book was' : `${res.added} books were`} added, and new ones will be too.`
              : `Following ${series.name}. New books in it are added by themselves.`),
        )
        const fresh = await api.bookSeries(workKey, title, author)
        setSeries(fresh.series)
      } else {
        await api.unfollowSeries(series.source, series.key)
        toast.success(`Stopped following ${series.name}. Its books stay in your library.`)
        setSeries({ ...series, followed: false })
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (series === null || (series && series.entries.length < 2)) return null
  const shown = (series?.entries ?? []).slice(0, cols * rows)
  return (
    <section className="discover-rail book-series">
      <div className="rail-head">
        <h2>
          {series ? (series.position ? `Book ${series.position} of ${series.name}` : series.name) : 'Series'}
          {series && <span className="rail-hint">{series.entries.length} books</span>}
        </h2>
        {series && (
          <Switch
            checked={series.followed}
            onChange={(v) => void follow(v)}
            disabled={busy}
            label="Follow this series"
            description="The missing books are added now, and new ones when they come out."
          />
        )}
      </div>
      <div className="poster-grid" ref={grid}>
        {series === undefined
          ? Array.from({ length: cols }, (_, i) => <div key={i} className="skeleton" style={{ aspectRatio: '2 / 3' }} />)
          : shown.map((e) => <BookCard key={`${e.position}-${e.key ?? e.title}`} book={asFound(e)} meta={entryMeta(e)} onAdd={setAdding} />)}
      </div>
      {series && series.entries.length > shown.length && (
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
