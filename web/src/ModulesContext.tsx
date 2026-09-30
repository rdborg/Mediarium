import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, type ModuleKey, type Modules } from './api'
import { useAuth } from './AuthContext'
import { useLive } from './useLive'

// Until the answer arrives (or when it cannot be read) the app behaves as it
// always did: movies and TV on, music off. That way nothing flickers away on
// a slow connection and an older server keeps working.
const FALLBACK: Modules = {
  movies: { enabled: true, available: true },
  tv: { enabled: true, available: true },
  music: { enabled: false, available: true },
  audiobooks: { enabled: false, available: false },
  ebooks: { enabled: false, available: false },
}

interface ModulesState {
  modules: Modules
  loaded: boolean
  // Whether the subtitles switch in Settings is on. Off until the server says so.
  subtitlesOn: boolean
  // Whether a kind of media is switched on.
  on: (key: ModuleKey) => boolean
  refresh: () => Promise<void>
  // Replaces the whole picture (used right after the switchboard saved).
  set: (next: Modules) => void
}

const ModulesContext = createContext<ModulesState | null>(null)

// Which kinds of media are switched on, for the whole app. Loaded once after
// sign-in (every account can read it) and refreshed whenever the switchboard
// changes something, or the browser tab comes back into view.
export function ModulesProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const signedIn = !!user
  const [modules, setModules] = useState<Modules>(FALLBACK)
  const [subtitlesOn, setSubtitlesOn] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const refresh = useCallback(async () => {
    if (!signedIn) return
    try {
      const { subtitlesEnabled, ...kinds } = await api.modules()
      setModules({ ...FALLBACK, ...kinds })
      setSubtitlesOn(subtitlesEnabled === true)
    } catch {
      // Keep whatever we had: an older server has no switchboard.
    } finally {
      setLoaded(true)
    }
  }, [signedIn])

  useEffect(() => {
    if (!signedIn) {
      setModules(FALLBACK)
      setSubtitlesOn(false)
      setLoaded(false)
      return
    }
    void refresh()
  }, [signedIn, refresh])
  useLive(refresh, 60000)

  const value = useMemo<ModulesState>(
    () => ({
      modules,
      loaded,
      subtitlesOn,
      on: (key) => modules[key]?.enabled === true,
      refresh,
      set: (next) => {
        setModules(next)
        setLoaded(true)
      },
    }),
    [modules, loaded, subtitlesOn, refresh],
  )
  return <ModulesContext.Provider value={value}>{children}</ModulesContext.Provider>
}

export function useModules() {
  const ctx = useContext(ModulesContext)
  if (!ctx) throw new Error('useModules must be used within ModulesProvider')
  return ctx
}

export type MediaKind = 'movie' | 'tv' | 'music'

// The kinds of media that are switched on, in the order they are shown.
export function useKinds(): MediaKind[] {
  const { on } = useModules()
  const kinds: MediaKind[] = []
  if (on('movies')) kinds.push('movie')
  if (on('tv')) kinds.push('tv')
  if (on('music')) kinds.push('music')
  return kinds.length > 0 ? kinds : ['movie']
}

export const KIND_LABEL: Record<MediaKind, string> = { movie: 'Movies', tv: 'TV shows', music: 'Music' }
