// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  agoText,
  countText,
  emptyText,
  filtersActive,
  happenedText,
  NO_FILTERS,
  problemQuery,
  problemText,
  rangeBounds,
  showingText,
  timesText,
  type ProblemFilters,
  type ProblemRow,
} from './problemsView.ts'

const NOW = new Date('2026-09-30T12:00:00Z')

const row = (over: Partial<ProblemRow> = {}): ProblemRow => ({
  id: 1,
  level: 'warning',
  area: 'usenet',
  areaLabel: 'Usenet',
  code: 'usenet.too_many_connections',
  title: 'Too many connections to your Usenet provider',
  message: 'news.example.com says this login has too many connections.',
  explain: 'Your Usenet provider says this login has too many connections open at once.',
  try: 'Stop the other program that uses the same account.',
  detail: 'limit now 3',
  count: 14,
  firstAt: '2026-09-30T11:00:00Z',
  lastAt: '2026-09-30T11:58:00Z',
  read: false,
  known: true,
  ...over,
})

test('ago text', () => {
  const at = (secondsAgo: number) => new Date(NOW.getTime() - secondsAgo * 1000).toISOString()
  const table: [number, string][] = [
    [5, 'just now'],
    [60, '1 minute ago'],
    [120, '2 minutes ago'],
    [3540, '59 minutes ago'],
    [3600, '1 hour ago'],
    [5 * 3600, '5 hours ago'],
    [47 * 3600, '47 hours ago'],
    [3 * 86400, '3 days ago'],
  ]
  for (const [secs, want] of table) assert.equal(agoText(at(secs), NOW.getTime()), want, `${secs}s`)
  assert.equal(agoText('nonsense', NOW.getTime()), '')
})

test('how often it happened', () => {
  assert.equal(timesText(1), 'once')
  assert.equal(timesText(0), 'once')
  assert.equal(timesText(14), '14 times')
  assert.equal(happenedText(row(), NOW.getTime()), 'Happened 14 times, last 2 minutes ago')
  assert.equal(happenedText(row({ count: 1 }), NOW.getTime()), 'Happened 2 minutes ago')
})

test('the words on the cards', () => {
  assert.equal(countText(0, 'error'), 'No errors')
  assert.equal(countText(1, 'warning'), '1 warning')
  assert.equal(countText(7, 'error'), '7 errors')
})

test('date ranges', () => {
  assert.deepEqual(rangeBounds({ range: 'any', from: '', to: '' }, NOW), {})
  assert.deepEqual(rangeBounds({ range: 'day', from: '', to: '' }, NOW), { from: '2026-09-29T12:00:00.000Z' })
  assert.deepEqual(rangeBounds({ range: 'week', from: '', to: '' }, NOW), { from: '2026-09-23T12:00:00.000Z' })
  assert.deepEqual(rangeBounds({ range: 'month', from: '', to: '' }, NOW), { from: '2026-08-31T12:00:00.000Z' })
  assert.deepEqual(rangeBounds({ range: 'custom', from: '2026-09-01', to: '' }, NOW), { from: '2026-09-01', to: undefined })
  // Dates typed in while another range is chosen do nothing.
  assert.deepEqual(rangeBounds({ range: 'any', from: '2026-09-01', to: '2026-09-02' }, NOW), {})
})

test('the query string has only what is filled in', () => {
  const f = (over: Partial<ProblemFilters>): ProblemFilters => ({ ...NO_FILTERS, ...over })
  const table: [string, ProblemFilters, Record<string, string | number>, string][] = [
    ['nothing', f({}), {}, ''],
    ['level', f({ level: 'error' }), {}, '?level=error'],
    ['area and text', f({ area: 'search', text: '  nzbgeek ' }), {}, '?area=search&q=nzbgeek'],
    ['unread', f({ unreadOnly: true }), { limit: 50 }, '?unread=1&limit=50'],
    ['custom dates', f({ range: 'custom', from: '2026-09-01', to: '2026-09-30' }), {}, '?from=2026-09-01&to=2026-09-30'],
    ['a preset', f({ range: 'day' }), {}, '?from=2026-09-29T12%3A00%3A00.000Z'],
    ['blank text', f({ text: '   ' }), {}, ''],
  ]
  for (const [name, filters, extra, want] of table) assert.equal(problemQuery(filters, extra, NOW), want, name)
})

test('filters count as active only when something is chosen', () => {
  assert.equal(filtersActive(NO_FILTERS), false)
  assert.equal(filtersActive({ ...NO_FILTERS, text: ' ' }), false)
  assert.equal(filtersActive({ ...NO_FILTERS, text: 'x' }), true)
  assert.equal(filtersActive({ ...NO_FILTERS, unreadOnly: true }), true)
  assert.equal(filtersActive({ ...NO_FILTERS, range: 'week' }), true)
})

test('what is shown when the list is empty or paged', () => {
  assert.equal(emptyText(false, 30), 'No problems in the last 30 days.')
  assert.equal(emptyText(true, 30), 'Nothing matches these filters.')
  assert.equal(showingText(0, 0), '')
  assert.equal(showingText(1, 1), '1 problem')
  assert.equal(showingText(50, 50), '50 problems')
  assert.equal(showingText(50, 132), 'Showing 50 of 132')
})

test('copying one problem', () => {
  const text = problemText(row({ forTitle: 'The Matrix' }), NOW.getTime())
  for (const s of [
    'Warning: Too many connections to your Usenet provider (Usenet, usenet.too_many_connections)',
    'Happened 14 times, last 2 minutes ago',
    'news.example.com says this login has too many connections.',
    'For: The Matrix',
    'What happened:\nYour Usenet provider says',
    'What to try:\nStop the other program',
    'Technical detail:\nlimit now 3',
  ]) assert.ok(text.includes(s), `missing ${JSON.stringify(s)} in:\n${text}`)

  const bare = problemText(row({ known: false, explain: undefined, try: undefined, detail: undefined, message: 'Something broke', title: 'Something broke', level: 'error' }), NOW.getTime())
  assert.ok(bare.startsWith('Error: Something broke'))
  assert.ok(!bare.includes('What to try'))
  assert.ok(!bare.includes('Technical detail'))
})
