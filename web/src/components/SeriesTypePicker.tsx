import { useState } from 'react'
import { api, can, type SeriesType } from '../api'
import { useAuth } from '../AuthContext'
import { useToast } from './Toast'

// On a show's page: how releases number its episodes. Anime releases count
// from the first episode ("Show - 105"); daily shows go by air date.
const LABEL: Record<SeriesType, string> = { standard: 'Seasons (S01E05)', anime: 'Anime (episode 105)', daily: 'Daily (by air date)' }

export default function SeriesTypePicker({ id, value, onChange }: { id: number; value: SeriesType; onChange: (t: SeriesType) => void }) {
  const toast = useToast()
  const me = useAuth().user
  const [busy, setBusy] = useState(false)
  async function change(t: SeriesType) {
    setBusy(true)
    try {
      await api.setSeriesType(id, t)
      onChange(t)
      toast.success(t === 'standard' ? 'Releases are matched by season and episode.' : t === 'anime' ? 'Releases are matched by episode number counted from the start, as anime is named.' : 'Releases are matched by the day the episode aired.')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <label className="inline-field series-type" title="How release names number this show's episodes">
      Episode numbering
      <select value={value} disabled={busy || !can(me, 'manage')} onChange={(e) => void change(e.target.value as SeriesType)}>
        {(Object.keys(LABEL) as SeriesType[]).map((t) => (
          <option key={t} value={t}>
            {LABEL[t]}
          </option>
        ))}
      </select>
    </label>
  )
}
