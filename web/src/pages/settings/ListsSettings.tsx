import { useCallback, useEffect, useState } from 'react'
import { api, type Settings } from '../../api'
import ImportLists from '../../components/ImportLists'
import ServiceKeyCard from '../../components/ServiceKeyCard'
import SimklConnect from '../../components/SimklConnect'
import { t } from '../../i18n'
import { SIMKL_COPY, TRAKT_COPY } from '../../components/serviceCopy'

// Settings > Library and files > Import lists: the lists, and the
// Trakt Client ID that reading Trakt lists needs, and the Simkl Client ID and
// account that reading a person's own Simkl lists needs.
export default function ListsSettings() {
  const [s, setS] = useState<Settings | null>(null)
  const load = useCallback(() => {
    api.getSettings().then(setS).catch(() => undefined)
  }, [])
  useEffect(load, [load])
  return (
    <div className="settings-stack">
      <ImportLists />
      {s && <ServiceKeyCard service="trakt" {...TRAKT_COPY} builtIn={!!s.traktClientIdBuiltIn} configured={s.hasTraktClientId} onSaved={load} allowOverride usingOwnKey={!!s.traktUsingOwnKey} />}
      {s && (
        <ServiceKeyCard service="simkl" {...SIMKL_COPY} savedLabel={t('Client ID saved')} builtIn={!!s.simklClientIdBuiltIn} configured={!!s.hasSimklClientId} onSaved={load} allowOverride usingOwnKey={!!s.simklUsingOwnKey}>
          <SimklConnect hasClientId={!!s.hasSimklClientId} />
        </ServiceKeyCard>
      )}
    </div>
  )
}
