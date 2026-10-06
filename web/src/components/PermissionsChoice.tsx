import { DEFAULT_PERMISSIONS, type Permission, type Permissions } from '../api'
import { useModules } from '../ModulesContext'

// What a basic account may do, chosen when adding or editing it. Everything
// is on by default, which is what a basic account could always do.

const ROWS: { key: Permission; label: string; hint: string }[] = [
  { key: 'addDirect', label: 'Add titles themselves', hint: 'Off: what they add becomes a request you approve or decline under Activity > Requests.' },
  { key: 'releases', label: 'Pick releases', hint: 'Search your indexers and choose what to download.' },
  { key: 'manage', label: 'Search now and monitoring', hint: 'Start searches, change what is monitored and tags, follow authors and series.' },
  { key: 'retry', label: 'Retry downloads', hint: 'Try a failed or stopped download again.' },
  { key: 'subtitles', label: 'Subtitles', hint: 'Find and download subtitles.' },
  { key: 'play', label: 'Play, read and listen', hint: 'Play videos, preview files, and read and listen in Mediarium Books.' },
]

export default function PermissionsChoice({ value, onChange, disabled }: { value: Permissions | undefined; onChange: (p: Permissions) => void; disabled?: boolean }) {
  const { on } = useModules()
  const p = value ?? DEFAULT_PERMISSIONS
  const set = (k: Permission, v: boolean) => onChange({ ...p, [k]: v })
  const kinds: { key: Permission; label: string; show: boolean }[] = [
    { key: 'movies', label: 'Movies', show: on('movies') },
    { key: 'tv', label: 'TV shows', show: on('tv') },
    { key: 'music', label: 'Music', show: on('music') },
    { key: 'books', label: 'Books', show: on('ebooks') || on('audiobooks') },
  ]
  return (
    <div className="perm-choice">
      <span className="field-title">Permissions</span>
      <div className="perm-kinds" role="group" aria-label="What they can add or ask for">
        <span className="field-note">They can add or ask for:</span>
        {kinds
          .filter((k) => k.show)
          .map((k) => (
            <label key={k.key} className="perm-kind">
              <input type="checkbox" checked={p[k.key]} disabled={disabled} onChange={(e) => set(k.key, e.target.checked)} /> {k.label}
            </label>
          ))}
      </div>
      <div className="perm-rows">
        {ROWS.map((r) => (
          <label key={r.key} className="perm-row">
            <input type="checkbox" checked={p[r.key]} disabled={disabled} onChange={(e) => set(r.key, e.target.checked)} />
            <span>
              <strong>{r.label}</strong>
              <small>{r.hint}</small>
            </span>
          </label>
        ))}
      </div>
    </div>
  )
}
