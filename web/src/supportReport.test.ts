// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatSupportReport, type Diagnostics } from './supportReport.ts'

function sample(over: Partial<Diagnostics> = {}): Diagnostics {
  return {
    version: '1.2.0',
    goVersion: 'go1.27.1',
    os: 'linux',
    arch: 'amd64',
    now: '2026-09-29T18:00:00Z',
    uptimeSeconds: 3 * 3600 + 120,
    safeMode: false,
    goroutines: 41,
    memoryBytes: 76 * 1024 * 1024,
    database: { journalMode: 'wal', sizeBytes: 12 * 1024 * 1024, walBytes: 512 * 1024, maxOpen: 8, open: 3, inUse: 1, idle: 2, waitCount: 4, waitSeconds: '120ms' },
    downloadsRunning: 2,
    usenet: [{ server: 'news.example.com:563', inUse: 4, limit: 10, configured: 10 }],
    health: [{ id: 'no-vpn', level: 'warn', title: 'Torrenting without a VPN' }],
    log: ['2026/09/29 18:00:00 Mediarium listening on :8264'],
    ...over,
  }
}

const table: { name: string; input: Diagnostics; has: string[]; lacks?: string[] }[] = [
  {
    name: 'a normal install',
    input: sample(),
    has: [
      'Mediarium support report',
      'Version: 1.2.0 (linux/amd64, go1.27.1)',
      'Running for: 3 hours',
      'Safe mode: off',
      'Memory in use: 76.0 MB, background tasks: 41',
      'Database: fast mode, 12.0 MB (log file 512 KB)',
      'Database connections: 1 of 8 in use, 2 idle, 4 waits so far (120ms in total)',
      'Downloads running: 2',
      'Usenet news.example.com:563: 4 of 10 connections in use',
      '  [warn] Torrenting without a VPN',
      'Recent log (1 lines):',
      '2026/09/29 18:00:00 Mediarium listening on :8264',
    ],
  },
  {
    name: 'the database could not use its faster mode',
    input: sample({ database: { ...sample().database, journalMode: 'delete', problem: 'SQLite stayed in "memory" mode' } }),
    has: ['Database: older, slower mode, 12.0 MB (SQLite stayed in "memory" mode)'],
    lacks: ['fast mode'],
  },
  {
    name: 'safe mode with nothing to report',
    input: sample({ safeMode: true, health: [], log: [], downloadsRunning: undefined, uptimeSeconds: 45 }),
    has: ['Safe mode: on', 'Running for: 45 seconds', 'Setup warnings:\n  none', 'Recent log (0 lines):\n  nothing yet'],
    lacks: ['Downloads running'],
  },
  {
    name: 'the provider limit lowered the connections',
    input: sample({ usenet: [{ server: 'news.example.com:563', inUse: 3, limit: 3, configured: 20 }] }),
    has: ['Usenet news.example.com:563: 3 of 3 connections in use (set to 20, lowered because the provider said too many)'],
  },
  {
    name: 'an update is installed and pushing is on',
    input: sample({
      update: {
        image: '1.1.0', installed: '1.2.0', latest: '1.3.0', checkedAt: '2026-10-01T10:00:00Z', check: true, autoInstall: false,
        allowPush: true, autoRestartWhenStuck: true, supervisor: 'docker', selfRestart: '2026-10-01T09:00:00Z: the web server answered: false',
      },
    }),
    has: [
      'Updates: image 1.1.0, installed update 1.2.0, newest known 1.3.0 (checked 2026-10-01T10:00:00Z)',
      'Update settings: daily check on, overnight install off, pushed updates ON, restart when stuck on, restarted by docker',
      'Restarted itself: 2026-10-01T09:00:00Z: the web server answered: false',
    ],
  },
  {
    name: 'a check that could not reach GitHub',
    input: sample({ update: { check: true, autoInstall: false, allowPush: false, autoRestartWhenStuck: true, supervisor: 'none', checkError: "Couldn't check just now." } }),
    has: ['Updates: image not used, installed update none, newest known unknown', "Couldn't check just now.", 'pushed updates off'],
    lacks: ['Restarted itself'],
  },
  {
    name: 'an older server with no update row',
    input: sample(),
    has: ['Mediarium support report'],
    lacks: ['Updates:'],
  },
  {
    name: 'problems from the problem log',
    input: sample({
      problems: [
        { at: '2026-09-29T17:55:00Z', level: 'warning', area: 'Usenet', code: 'usenet.too_many_connections', message: 'news.example.com says this login has too many connections.', count: 14 },
        { at: '2026-09-29T17:00:00Z', level: 'error', area: 'Downloads', code: 'unpack.failed', message: 'Could not unpack a download', count: 1, detail: 'bad header\nsecond line' },
      ],
    }),
    has: [
      'Recent problems (2):',
      '  [2026-09-29T17:55:00Z] WARNING Usenet, usenet.too_many_connections, 14 times: news.example.com says this login has too many connections.',
      '  [2026-09-29T17:00:00Z] ERROR Downloads, unpack.failed: Could not unpack a download',
      '      bad header\n      second line',
    ],
  },
  {
    name: 'no problems recorded',
    input: sample({ problems: [] }),
    has: ['Recent problems (0):\n  none'],
  },
  {
    name: 'an older server without a problem log',
    input: sample(),
    has: ['Mediarium support report'],
    lacks: ['Recent problems'],
  },
  {
    name: 'running for days',
    input: sample({ uptimeSeconds: 5 * 86400 }),
    has: ['Running for: 5 days'],
  },
]

for (const tc of table) {
  test(`support report: ${tc.name}`, () => {
    const text = formatSupportReport(tc.input)
    for (const s of tc.has) assert.ok(text.includes(s), `missing ${JSON.stringify(s)} in:\n${text}`)
    for (const s of tc.lacks ?? []) assert.ok(!text.includes(s), `should not contain ${JSON.stringify(s)}`)
  })
}
