import { useEffect, useState } from 'react'
import { api, type QualityProfile, type SourcePref } from '../api'
import { useToast } from './Toast'

const SOURCE_NAME: Record<string, string> = { both: 'Usenet and torrents', usenet: 'Usenet only', torrent: 'Torrents only' }

// Per-item download choices for a movie or a show: which quality profile it
// follows and which downloaders it may use. "Default" follows Settings.
export default function ProfilePicker({ kind, itemId }: { kind: 'movie' | 'series'; itemId: number }) {
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultId, setDefaultId] = useState(0)
  const [defaultSources, setDefaultSources] = useState<SourcePref>('both')
  const [current, setCurrent] = useState<number | null>(null)
  const [sources, setSources] = useState<SourcePref>('')
  const toast = useToast()

  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(r.profiles)
      setDefaultId(r.defaultId)
    }).catch(() => undefined)
    api.getSettings().then((s) => setDefaultSources(s.defaultSources ?? 'both')).catch(() => undefined)
    const load = kind === 'movie' ? api.getMovie(itemId) : api.getSeries(itemId)
    load
      .then((item) => {
        setCurrent(item.profileId ?? 0)
        setSources(item.sources ?? '')
      })
      .catch(() => undefined)
  }, [kind, itemId])

  async function chooseProfile(id: number) {
    const previous = current
    setCurrent(id)
    try {
      if (kind === 'movie') await api.setMovieProfile(itemId, id)
      else await api.setSeriesProfile(itemId, id)
      toast.success('Quality profile saved.')
    } catch (e) {
      setCurrent(previous)
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function chooseSources(next: SourcePref) {
    const previous = sources
    setSources(next)
    try {
      if (kind === 'movie') await api.setMovieSources(itemId, next)
      else await api.setSeriesSources(itemId, next)
      toast.success(next ? `Now downloading from: ${SOURCE_NAME[next]}.` : 'Now following the default downloaders.')
    } catch (e) {
      setSources(previous)
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (current === null || profiles.length === 0) return null
  const defaultName = profiles.find((p) => p.id === defaultId)?.name ?? 'default'

  return (
    <div className="picker-row">
      <label>
        Quality profile
        <select value={current} onChange={(e) => void chooseProfile(Number(e.target.value))}>
          <option value={0}>Default ({defaultName})</option>
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <label>
        Download from
        <select value={sources} onChange={(e) => void chooseSources(e.target.value as SourcePref)}>
          <option value="">Default ({SOURCE_NAME[defaultSources] ?? 'Usenet and torrents'})</option>
          <option value="both">{SOURCE_NAME.both}</option>
          <option value="usenet">{SOURCE_NAME.usenet}</option>
          <option value="torrent">{SOURCE_NAME.torrent}</option>
        </select>
      </label>
    </div>
  )
}
