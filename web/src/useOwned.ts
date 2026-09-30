import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { movieState, seriesState, type ItemStateInput } from './components/state'
import { useLive } from './useLive'

export interface Owned {
  movies: Map<number, ItemStateInput>
  shows: Map<number, ItemStateInput>
  reload: () => void
}

// Which TMDB titles are already in the library, and their state (wanted,
// downloading, downloaded), kept live so Discover cards update on their own.
export function useOwned(ms = 15000): Owned {
  const [movies, setMovies] = useState<Map<number, ItemStateInput>>(new Map())
  const [shows, setShows] = useState<Map<number, ItemStateInput>>(new Map())

  const reload = useCallback(() => {
    api
      .listQueue()
      .catch(() => [])
      .then((queue) => {
        api.listMovies().then((m) => setMovies(new Map(m.map((x) => [x.tmdbId, { id: x.id, state: movieState(x, queue) }])))).catch(() => undefined)
        api.listSeries().then((s) => setShows(new Map(s.map((x) => [x.tmdbId, { id: x.id, state: seriesState(x, queue) }])))).catch(() => undefined)
      })
  }, [])

  useEffect(reload, [reload])
  useLive(reload, ms)
  return { movies, shows, reload }
}
