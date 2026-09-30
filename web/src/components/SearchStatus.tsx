import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, isAdmin, type TitleEvent } from '../api'
import { useAuth } from '../AuthContext'
import { timeAgo } from '../format'
import { useLive } from '../useLive'
import Icon from './Icon'

// For a title still waiting for a release: what the last search found and
// why nothing was taken, in plain words, with what can be done about it.
export default function SearchStatus({ kind, id }: { kind: 'movie' | 'series' | 'album'; id: number }) {
  const { user } = useAuth()
  const [last, setLast] = useState<TitleEvent | null>(null)

  const load = useCallback(() => {
    ;(kind === 'movie' ? api.movieEvents(id) : kind === 'album' ? api.albumEvents(id) : api.seriesEvents(id))
      .then((ev) => setLast(ev.find((e) => e.kind === 'searched') ?? null))
      .catch(() => setLast(null))
  }, [kind, id])
  useEffect(load, [load])
  useLive(load, 10000)

  if (!last) return null
  const nothing = last.level !== 'info'
  const cinema = /CAM\/TeleSync/.test(last.message)
  return (
    <div className={`search-status${nothing ? ' waiting' : ''}`}>
      <Icon name={nothing ? 'clock' : 'check'} size={16} />
      <div>
        <strong>{nothing ? 'Waiting for a release' : 'Last search'}</strong> <small>· {timeAgo(last.at)}</small>
        <p>{last.message.replace(/^Searched: /, '')}</p>
        {nothing && (
          <p className="search-status-hint">
            Mediarium keeps looking and grabs it when a release you accept turns up.
            {cinema && " Only cinema recordings exist so far, which your quality profile doesn't accept."}
            {isAdmin(user) && (
              <>
                {' '}
                <Link to="/settings/quality">Choose other qualities to fall back to</Link>
              </>
            )}
          </p>
        )}
      </div>
    </div>
  )
}
