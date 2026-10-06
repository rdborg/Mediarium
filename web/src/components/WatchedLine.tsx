import { useEffect, useState } from 'react'
import { api, type WatchedTitle } from '../api'
import Icon from './Icon'

// "Watched 2 times · last on 3 Mar 2026" on a movie's or show's page, when
// reading what's been watched is on (Settings > Media servers).

let cache: { at: number; data: Promise<Awaited<ReturnType<typeof api.watched>>> } | null = null

function watchedData() {
  if (!cache || Date.now() - cache.at > 60_000) cache = { at: Date.now(), data: api.watched() }
  return cache.data
}

export function watchedText(kind: 'movie' | 'tv', w: WatchedTitle): string {
  const last = w.lastPlayed ? new Date(w.lastPlayed) : null
  const on = last && !Number.isNaN(last.getTime()) ? ` · last on ${last.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })}` : ''
  if (kind === 'tv') return `${w.episodes === 1 ? '1 episode' : `${w.episodes ?? 0} episodes`} watched${on}`
  return `Watched${w.plays > 1 ? ` ${w.plays} times` : ''}${on}`
}

export default function WatchedLine({ kind, id }: { kind: 'movie' | 'tv'; id: number }) {
  const [w, setW] = useState<WatchedTitle | null>(null)
  useEffect(() => {
    let live = true
    watchedData()
      .then((d) => live && setW((kind === 'movie' ? d.movies : d.series)[String(id)] ?? null))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [kind, id])
  if (!w) return null
  return (
    <p className="watched-line">
      <Icon name="eye" size={13} /> {watchedText(kind, w)}
    </p>
  )
}
