import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../../api'
import ServiceKeyCard from '../../components/ServiceKeyCard'
import { TMDB_COPY, TRAKT_COPY } from '../../components/serviceCopy'

export default function MetadataSettings() {
  const [s, setS] = useState<SettingsData | null>(null)
  const load = useCallback(() => {
    api.getSettings().then(setS).catch(() => undefined)
  }, [])
  useEffect(load, [load])

  if (!s) return <p>Loading…</p>
  return (
    <div className="settings-stack">
      <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={!!s.tmdbKeyBuiltIn} configured={s.hasTmdbApiKey} onSaved={load} />
      <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={!!s.traktClientIdBuiltIn} configured={s.hasTraktClientId} onSaved={load} />
    </div>
  )
}
