// The text a person pastes into a support request: what Mediarium reports about
// itself (GET /api/system/diagnostics) written out in plain lines. The server
// has already taken passwords, keys and tokens out of the log lines.

export interface Diagnostics {
  version: string
  goVersion: string
  os: string
  arch: string
  now: string
  uptimeSeconds: number
  safeMode: boolean
  goroutines: number
  memoryBytes: number
  database: {
    journalMode: string
    problem?: string
    sizeBytes: number
    walBytes: number
    maxOpen: number
    open: number
    inUse: number
    idle: number
    waitCount: number
    waitSeconds: string
  }
  // How updating is set up; left out by servers from before it existed.
  update?: {
    image?: string
    installed?: string
    latest?: string
    checkedAt?: string
    checkError?: string
    check: boolean
    autoInstall: boolean
    allowPush: boolean
    autoRestartWhenStuck: boolean
    supervisor: string
    selfRestart?: string
  }
  downloadsRunning?: number
  // News server logins used since the app started, and how many connections each may open.
  usenet?: { server: string; inUse: number; limit: number; configured: number }[]
  health: { id: string; level: string; title: string }[]
  // The latest rows of the problem log (Logs and errors); left out by older servers.
  problems?: { at: string; level: string; area: string; code: string; message: string; count: number; detail?: string }[]
  log: string[]
}

function size(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function duration(seconds: number): string {
  if (seconds < 90) return `${seconds} seconds`
  const minutes = Math.round(seconds / 60)
  if (minutes < 90) return `${minutes} minutes`
  const hours = Math.round(seconds / 3600)
  if (hours < 48) return `${hours} hours`
  return `${Math.round(seconds / 86400)} days`
}

export function formatSupportReport(d: Diagnostics): string {
  const db = d.database
  const lines = [
    'Mediarium support report',
    `Version: ${d.version} (${d.os}/${d.arch}, ${d.goVersion})`,
    `Time: ${d.now}`,
    `Running for: ${duration(d.uptimeSeconds)}`,
    `Safe mode: ${d.safeMode ? 'on' : 'off'}`,
    `Memory in use: ${size(d.memoryBytes)}, background tasks: ${d.goroutines}`,
    db.journalMode === 'wal'
      ? `Database: fast mode, ${size(db.sizeBytes)} (log file ${size(db.walBytes)})`
      : `Database: older, slower mode, ${size(db.sizeBytes)}${db.problem ? ` (${db.problem})` : ''}`,
    `Database connections: ${db.inUse} of ${db.maxOpen} in use, ${db.idle} idle, ${db.waitCount} waits so far (${db.waitSeconds} in total)`,
  ]
  if (d.downloadsRunning !== undefined) lines.push(`Downloads running: ${d.downloadsRunning}`)
  const up = d.update
  if (up) {
    const onOff = (v: boolean) => (v ? 'on' : 'off')
    lines.push(
      `Updates: image ${up.image || 'not used'}, installed update ${up.installed || 'none'}, newest known ${up.latest || 'unknown'}${up.checkedAt ? ` (checked ${up.checkedAt})` : ''}${up.checkError ? `, ${up.checkError}` : ''}`,
      `Update settings: daily check ${onOff(up.check)}, overnight install ${onOff(up.autoInstall)}, pushed updates ${up.allowPush ? 'ON' : 'off'}, restart when stuck ${onOff(up.autoRestartWhenStuck)}, restarted by ${up.supervisor}`,
    )
    if (up.selfRestart) lines.push(`Restarted itself: ${up.selfRestart}`)
  }
  for (const u of d.usenet ?? []) {
    const lowered = u.limit < u.configured ? ` (set to ${u.configured}, lowered because the provider said too many)` : ''
    lines.push(`Usenet ${u.server}: ${u.inUse} of ${u.limit} connections in use${lowered}`)
  }
  lines.push('', 'Setup warnings:')
  if (d.health.length === 0) lines.push('  none')
  for (const h of d.health) lines.push(`  [${h.level}] ${h.title}`)
  if (d.problems) {
    lines.push('', `Recent problems (${d.problems.length}):`)
    if (d.problems.length === 0) lines.push('  none')
    for (const p of d.problems) {
      lines.push(`  [${p.at}] ${p.level.toUpperCase()} ${p.area}, ${p.code}${p.count > 1 ? `, ${p.count} times` : ''}: ${p.message}`)
      if (p.detail) lines.push(...p.detail.split('\n').map((l) => `      ${l}`))
    }
  }
  lines.push('', `Recent log (${d.log.length} lines):`)
  if (d.log.length === 0) lines.push('  nothing yet')
  lines.push(...d.log)
  return lines.join('\n')
}
