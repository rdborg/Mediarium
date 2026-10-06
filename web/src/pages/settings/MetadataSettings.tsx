import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../../api'
import HardcoverCard from '../../components/HardcoverCard'
import ServiceKeyCard from '../../components/ServiceKeyCard'
import { TMDB_COPY, TRAKT_COPY } from '../../components/serviceCopy'

export default function MetadataSettings() {
  const [s, setS] = useState<SettingsData | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    api
      .getSettings()
      .then((data) => {
        setS(data)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  if (!s) {
    return error ? (
      <div>
        <p className="error-text">{error}</p>
        <button onClick={load}>Try again</button>
      </div>
    ) : (
      <p>Loading…</p>
    )
  }
  return (
    <div className="settings-stack">
      <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={!!s.tmdbKeyBuiltIn} configured={s.hasTmdbApiKey} onSaved={load} />
      <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={!!s.traktClientIdBuiltIn} configured={s.hasTraktClientId} onSaved={load} allowOverride usingOwnKey={!!s.traktUsingOwnKey} />
      <HardcoverCard />
    </div>
  )
}
