import type { BookFormat, BookFound, BookImportResult } from './api'

// Matching a book from the import list by hand.

// A book can be looked up by hand while the import has not found it (or could
// not save it).
export function canFindByHand(status: BookImportResult['status']): boolean {
  return status === 'unmatched' || status === 'failed'
}

// What the search box starts with: how the import read the name on disk.
export function suggestedQuery(r: Pick<BookImportResult, 'title' | 'author'>): string {
  return [r.title, r.author].map((p) => (p ?? '').trim()).filter(Boolean).join(' ')
}

// "The Hobbit · J.R.R. Tolkien · 1937", leaving out what the result lacks.
export function resultLine(f: Pick<BookFound, 'title' | 'author' | 'year'>): string {
  return [f.title, f.author, f.year ? String(f.year) : ''].filter(Boolean).join(' · ')
}

// The body of POST /api/books/import/match for one row and the book picked for it.
export function matchBody(r: Pick<BookImportResult, 'path' | 'kind'>, f: BookFound, rename: boolean) {
  return {
    path: r.path,
    kind: r.kind as BookFormat,
    olKey: f.key,
    title: f.title,
    author: f.author,
    authorKey: f.authorKey,
    year: f.year,
    coverId: f.coverId,
    ...(f.hardcoverId ? { hardcoverId: f.hardcoverId } : {}),
    ...(f.googleId ? { googleId: f.googleId } : {}),
    rename,
  }
}
