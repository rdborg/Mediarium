import assert from 'node:assert/strict'
import { test } from 'node:test'
import { closePaneTarget, escapeClosesPane, openFromList, paneSelected, paneTab, TWO_PANE_QUERY } from './twoPane.ts'

test('series, books and artists always sit beside the list', () => {
  assert.equal(paneSelected('/series/12', ''), true)
  assert.equal(paneSelected('/book/3', '?kind=audiobook'), true)
  assert.equal(paneSelected('/music/artist/9', ''), true)
  assert.equal(paneSelected('/series/12/', ''), true)
})

test('a movie page only gets the list when it was opened from it', () => {
  assert.equal(paneSelected('/title/603', ''), false)
  assert.equal(paneSelected('/title/603', '?lib=1'), true)
  assert.equal(paneSelected('/title/603', '?lib=0'), false)
})

test('other pages never get a second pane', () => {
  for (const p of ['/library', '/', '/discover', '/show/5', '/series/', '/music/artist/', '/music/album/4', '/music/artist/9/albums', '/books/work/OL1W', '/queue']) {
    assert.equal(paneSelected(p, '?lib=1'), false, p)
  }
})

test('the list tab comes from the address, then from the page', () => {
  assert.equal(paneTab('/series/1', ''), 'tv')
  assert.equal(paneTab('/series/1', '?kind=anime'), 'anime')
  assert.equal(paneTab('/book/1', ''), 'ebook')
  assert.equal(paneTab('/book/1', '?kind=audiobook'), 'audiobook')
  assert.equal(paneTab('/music/artist/1', '?kind=nonsense'), 'music')
  assert.equal(paneTab('/title/1', '?lib=1'), 'movie')
})

test('opening from the list keeps its settings and adds the mark', () => {
  assert.equal(openFromList('/title/5', ''), '/title/5?lib=1')
  assert.equal(openFromList('/series/7', '?kind=tv&st=downloaded&q=the'), '/series/7?kind=tv&st=downloaded&q=the&lib=1')
  assert.equal(openFromList('/series/7', '?lib=1&q=a'), '/series/7?lib=1&q=a')
})

test('closing goes back to the list on the same tab with the same filters', () => {
  assert.equal(closePaneTarget('/title/5', '?lib=1'), '/library')
  assert.equal(closePaneTarget('/title/5', '?lib=1&st=downloaded'), '/library?st=downloaded')
  assert.equal(closePaneTarget('/series/7', '?q=the&lib=1'), '/library?q=the&kind=tv')
  assert.equal(closePaneTarget('/music/artist/2', ''), '/library?kind=music')
  assert.equal(closePaneTarget('/book/2', '?kind=audiobook&lib=1'), '/library?kind=audiobook')
})

test('Esc closes the pane unless something else is using it', () => {
  const base = { key: 'Escape', defaultPrevented: false, inField: false, overlayOpen: false }
  assert.equal(escapeClosesPane(base), true)
  assert.equal(escapeClosesPane({ ...base, key: 'Enter' }), false)
  assert.equal(escapeClosesPane({ ...base, defaultPrevented: true }), false)
  assert.equal(escapeClosesPane({ ...base, inField: true }), false)
  assert.equal(escapeClosesPane({ ...base, overlayOpen: true }), false)
})

test('the two-pane width is a tablet range that is tall enough', () => {
  assert.match(TWO_PANE_QUERY, /min-width: 768px/)
  assert.match(TWO_PANE_QUERY, /max-width: 1180px/)
  assert.match(TWO_PANE_QUERY, /min-height/)
})
