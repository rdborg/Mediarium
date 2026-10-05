// Demo mode for ebooks and audiobooks: public-domain classics with the same
// text-free artwork as the demo movies, so screenshots show no real covers.
// Used by demo.ts; nothing here is saved anywhere.
import type { Book, BookFound, BookProgress, BookTrack, BookWork } from './api'

// Text-free cover art: a book-like block of colour with a band and a mark.
export function bookCover(seed: number): string {
  const h1 = (seed * 53 + 20) % 360
  const h2 = (h1 + 150) % 360
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 300"><rect width="200" height="300" fill="hsl(${h1} 45% 30%)"/><rect x="0" y="0" width="14" height="300" fill="hsl(${h1} 45% 20%)"/><rect x="28" y="${60 + (seed % 4) * 12}" width="144" height="54" rx="4" fill="hsl(${h2} 55% 72%)" opacity=".9"/><circle cx="100" cy="${205 + (seed % 3) * 8}" r="${22 + (seed % 3) * 6}" fill="hsl(${h2} 70% 85%)" opacity=".5"/><rect x="28" y="262" width="${70 + (seed % 5) * 12}" height="8" rx="4" fill="hsl(${h2} 50% 85%)" opacity=".7"/></svg>`
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
}

const OV =
  'A sample description for demo mode. In the real app this is the book description from Open Library, a few sentences about what the book is about.'

interface Seed {
  id: number
  key: string
  title: string
  author: string
  authorKey: string
  year: number
  ebook: 'downloaded' | 'missing' | 'downloading' | ''
  audio: 'downloaded' | 'missing' | 'downloading' | ''
  ebookFormat?: string
  audioFormat?: string
  hasEbook?: boolean
  hasAudio?: boolean
}

const SEEDS: Seed[] = [
  { id: 1, key: 'OLD1W', title: 'Pride and Prejudice', author: 'Jane Austen', authorKey: 'OLD1A', year: 1813, ebook: 'downloaded', audio: 'downloaded', ebookFormat: 'epub', audioFormat: 'm4b' },
  { id: 2, key: 'OLD2W', title: 'Moby-Dick', author: 'Herman Melville', authorKey: 'OLD2A', year: 1851, ebook: 'downloaded', audio: '', ebookFormat: 'epub' },
  { id: 3, key: 'OLD3W', title: 'Frankenstein', author: 'Mary Shelley', authorKey: 'OLD3A', year: 1818, ebook: 'downloaded', audio: 'downloading', ebookFormat: 'epub' },
  { id: 4, key: 'OLD4W', title: 'The Adventures of Sherlock Holmes', author: 'Arthur Conan Doyle', authorKey: 'OLD4A', year: 1892, ebook: '', audio: 'downloaded', audioFormat: 'mp3' },
  { id: 5, key: 'OLD5W', title: 'Dracula', author: 'Bram Stoker', authorKey: 'OLD5A', year: 1897, ebook: 'missing', audio: 'missing' },
  { id: 6, key: 'OLD6W', title: 'The Time Machine', author: 'H. G. Wells', authorKey: 'OLD6A', year: 1895, ebook: 'downloaded', audio: '', ebookFormat: 'azw3' },
  { id: 7, key: 'OLD7W', title: 'Jane Eyre', author: 'Charlotte Brontë', authorKey: 'OLD7A', year: 1847, ebook: 'downloaded', audio: 'downloaded', ebookFormat: 'epub', audioFormat: 'm4b' },
  { id: 8, key: 'OLD8W', title: 'Treasure Island', author: 'Robert Louis Stevenson', authorKey: 'OLD8A', year: 1883, ebook: '', audio: 'missing' },
  { id: 9, key: 'OLD9W', title: 'Little Women', author: 'Louisa May Alcott', authorKey: 'OLD9A', year: 1868, ebook: 'downloaded', audio: '', ebookFormat: 'pdf' },
  { id: 10, key: 'OLD10W', title: 'The War of the Worlds', author: 'H. G. Wells', authorKey: 'OLD6A', year: 1898, ebook: 'missing', audio: 'downloaded', audioFormat: 'mp3' },
]

const POOL: Omit<Seed, 'id' | 'ebook' | 'audio'>[] = [
  { key: 'OLP1W', title: 'Wuthering Heights', author: 'Emily Brontë', authorKey: 'OLP1A', year: 1847, hasEbook: true, hasAudio: true },
  { key: 'OLP2W', title: 'Great Expectations', author: 'Charles Dickens', authorKey: 'OLP2A', year: 1861, hasEbook: true, hasAudio: true },
  { key: 'OLP3W', title: 'The Picture of Dorian Gray', author: 'Oscar Wilde', authorKey: 'OLP3A', year: 1890, hasEbook: true, hasAudio: false },
  { key: 'OLP4W', title: 'A Tale of Two Cities', author: 'Charles Dickens', authorKey: 'OLP2A', year: 1859, hasEbook: true, hasAudio: true },
  { key: 'OLP5W', title: 'The Call of the Wild', author: 'Jack London', authorKey: 'OLP5A', year: 1903, hasEbook: false, hasAudio: true },
  { key: 'OLP6W', title: 'Alice’s Adventures in Wonderland', author: 'Lewis Carroll', authorKey: 'OLP6A', year: 1865, hasEbook: true, hasAudio: true },
  { key: 'OLP7W', title: 'The Invisible Man', author: 'H. G. Wells', authorKey: 'OLD6A', year: 1897, hasEbook: true, hasAudio: false },
  { key: 'OLP8W', title: 'Twenty Thousand Leagues Under the Seas', author: 'Jules Verne', authorKey: 'OLP8A', year: 1870, hasEbook: true, hasAudio: true },
  { key: 'OLP9W', title: 'Around the World in Eighty Days', author: 'Jules Verne', authorKey: 'OLP8A', year: 1872, hasEbook: false, hasAudio: true },
  { key: 'OLP10W', title: 'The Hound of the Baskervilles', author: 'Arthur Conan Doyle', authorKey: 'OLD4A', year: 1902, hasEbook: true, hasAudio: true },
  { key: 'OLP11W', title: 'Emma', author: 'Jane Austen', authorKey: 'OLD1A', year: 1815, hasEbook: true, hasAudio: true },
  { key: 'OLP12W', title: 'Sense and Sensibility', author: 'Jane Austen', authorKey: 'OLD1A', year: 1811, hasEbook: true, hasAudio: false },
  { key: 'OLP13W', title: 'Heart of Darkness', author: 'Joseph Conrad', authorKey: 'OLP13A', year: 1899, hasEbook: true, hasAudio: true },
  { key: 'OLP14W', title: 'The Jungle Book', author: 'Rudyard Kipling', authorKey: 'OLP14A', year: 1894, hasEbook: false, hasAudio: true },
  { key: 'OLP15W', title: 'Persuasion', author: 'Jane Austen', authorKey: 'OLD1A', year: 1817, hasEbook: true, hasAudio: true },
  { key: 'OLP16W', title: 'Anne of Green Gables', author: 'L. M. Montgomery', authorKey: 'OLP16A', year: 1908, hasEbook: true, hasAudio: true },
]

const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()

export const demoBooks: Book[] = SEEDS.map((s) => ({
  id: s.id,
  olKey: s.key,
  title: s.title,
  author: s.author,
  authorKey: s.authorKey,
  year: s.year,
  coverUrl: bookCover(s.id),
  description: OV,
  addedAt: ago(s.id * 700),
  ebook: { wanted: s.ebook !== '', status: s.ebook === '' ? 'missing' : s.ebook, format: s.ebookFormat, path: s.ebookFormat ? `/data/Ebooks/${s.author}/${s.title} (${s.year})/${s.title}.${s.ebookFormat}` : undefined },
  audiobook: { wanted: s.audio !== '', status: s.audio === '' ? 'missing' : s.audio, format: s.audioFormat, path: s.audioFormat ? `/data/Audiobooks/${s.author}/${s.title} (${s.year})` : undefined },
}))

export const demoProgress: BookProgress[] = [
  { bookId: 1, format: 'ebook', position: '', percent: 42, finished: false, updatedAt: ago(40) },
  { bookId: 4, format: 'audiobook', position: '3:612', percent: 27, finished: false, updatedAt: ago(300) },
  { bookId: 7, format: 'audiobook', position: '11:95', percent: 68, finished: false, updatedAt: ago(1500) },
  { bookId: 2, format: 'ebook', position: '', percent: 8, finished: false, updatedAt: ago(4000) },
]

function asFound(b: Book | (typeof POOL)[number], i: number): BookFound {
  if ('olKey' in b) {
    return { key: b.olKey, title: b.title, author: b.author, authorKey: b.authorKey, year: b.year, coverUrl: b.coverUrl, libraryId: b.id, hasEbook: true, hasAudio: b.id % 3 !== 2 }
  }
  return { key: b.key, title: b.title, author: b.author, authorKey: b.authorKey, year: b.year, coverUrl: bookCover(100 + i), hasEbook: b.hasEbook, hasAudio: b.hasAudio }
}

// A Discover list: the pool in a different order per list, a few of them in the library.
export function demoBookList(seed: number, page: number): BookFound[] {
  if (page > 1) return []
  const all = [...POOL.map((b, i) => asFound(b, i)), ...demoBooks.slice(0, 4).map((b, i) => asFound(b, i))]
  const shift = (seed * 5) % all.length
  return [...all.slice(shift), ...all.slice(0, shift)]
}

export function demoBookSearch(q: string): BookFound[] {
  const n = q.trim().toLowerCase()
  return demoBookList(0, 1).filter((b) => b.title.toLowerCase().includes(n) || b.author.toLowerCase().includes(n)).slice(0, 10)
}

export function demoAuthorWorks(authorKey: string): BookFound[] {
  return demoBookList(0, 1).filter((b) => b.authorKey === authorKey)
}

export function demoWork(key: string): BookWork | null {
  const f = demoBookList(0, 1).find((b) => b.key === key)
  if (!f) return null
  return { ...f, description: OV, subjects: ['Fiction', 'Classics', 'Literature', 'Romance'] }
}

export const demoTracks: BookTrack[] = Array.from({ length: 14 }, (_, i) => ({ index: i, name: `${String(i + 1).padStart(2, '0')} - Chapter ${i + 1}`, size: 24e6 + (i % 4) * 3e6 }))

export const demoBookCounts = {
  ebooks: { books: demoBooks.filter((b) => b.ebook.wanted).length, downloaded: demoBooks.filter((b) => b.ebook.status === 'downloaded').length, missing: demoBooks.filter((b) => b.ebook.wanted && b.ebook.status === 'missing').length },
  audiobooks: { books: demoBooks.filter((b) => b.audiobook.wanted).length, downloaded: demoBooks.filter((b) => b.audiobook.status === 'downloaded').length, missing: demoBooks.filter((b) => b.audiobook.wanted && b.audiobook.status === 'missing').length },
}
