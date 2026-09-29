import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type TitleEvent } from '../api'
import { timeAgo } from '../format'
import { useLive } from '../useLive'
import Icon from './Icon'

const LEVEL_ICON: Record<TitleEvent['level'], string> = { info: 'info', warn: 'warning', error: 'warning' }

// "What happened": the searches, grabs, downloads, unpacking, imports and
// failures for one movie or show, newest first, so it is always clear why
// something is (or is not) downloading.
export default function TitleEvents({ kind, id }: { kind: 'movie' | 'series'; id: number }) {
  const [events, setEvents] = useState<TitleEvent[] | null>(null)
  const [missing, setMissing] = useState(false)
  const [all, setAll] = useState(false)

  const load = useCallback(() => {
    ;(kind === 'movie' ? api.movieEvents(id) : api.seriesEvents(id))
      .then(setEvents)
      .catch((e) => {
        if (e instanceof ApiError && e.status === 404) setMissing(true)
        else setEvents([])
      })
  }, [kind, id])
  useEffect(load, [load])
  useLive(load, 8000)

  if (missing) return null
  if (events === null) return <div className="skeleton" style={{ height: 90 }} />
  if (events.length === 0) return <p style={{ margin: 0, color: 'var(--text-dim)' }}>Nothing yet. Searches, downloads and imports for this title show up here.</p>

  const shown = all ? events : events.slice(0, 8)
  return (
    <>
      <ol className="title-events">
        {shown.map((e, i) => (
          <li key={`${e.at}-${i}`} className={`ev-${e.level}`}>
            <span className="ev-ico">
              <Icon name={LEVEL_ICON[e.level] ?? 'info'} size={14} />
            </span>
            <span className="ev-msg">{e.message}</span>
            <time dateTime={e.at} title={new Date(e.at).toLocaleString()}>
              {timeAgo(e.at)}
            </time>
          </li>
        ))}
      </ol>
      {events.length > 8 && (
        <button className="btn-sm" style={{ marginTop: 8 }} onClick={() => setAll((v) => !v)}>
          {all ? 'Show less' : `Show all ${events.length}`}
        </button>
      )}
    </>
  )
}
