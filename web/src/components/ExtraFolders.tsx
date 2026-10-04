import { useEffect, useState } from 'react'
import { api } from '../api'
import { useToast } from './Toast'

// Settings > Library: more movie or TV folders besides the main one, one per
// line (another disk, a "Kids" folder). When there are some, adding a title
// asks which folder to keep it in. Saved with its own button.
export default function ExtraFolders({ kind }: { kind: 'movies' | 'tv' }) {
  const toast = useToast()
  const key = kind === 'movies' ? 'moviesExtraPaths' : 'tvExtraPaths'
  const [saved, setSaved] = useState<string[] | null>(null)
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        const list = (s[key] as string[] | undefined) ?? []
        setSaved(list)
        setText(list.join('\n'))
      })
      .catch(() => setSaved([]))
  }, [key])

  const lines = text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
  const changed = saved !== null && lines.join('\n') !== saved.join('\n')

  async function save() {
    setBusy(true)
    try {
      const s = await api.putSettings({ [key]: lines })
      const list = (s[key] as string[] | undefined) ?? lines
      setSaved(list)
      setText(list.join('\n'))
      toast.success(list.length === 0 ? `Saved: only the main ${kind === 'movies' ? 'movies' : 'TV'} folder is used.` : `Saved: ${list.length} more ${kind === 'movies' ? 'movie' : 'TV'} ${list.length === 1 ? 'folder' : 'folders'}.`)
    } catch (e) {
      toast.error(`Not saved: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="path-col">
      <label>
        More {kind === 'movies' ? 'movie' : 'TV'} folders
        <textarea rows={2} value={text} onChange={(e) => setText(e.target.value)} placeholder={kind === 'movies' ? '/data/Movies-Kids' : '/data/TV-4K'} spellCheck={false} />
      </label>
      <small className="path-note">One per line, optional. When you add a title you choose which folder it goes in.</small>
      {changed && (
        <div>
          <button className="btn-sm" disabled={busy} onClick={() => void save()}>
            {busy ? 'Saving…' : 'Save folders'}
          </button>
        </div>
      )}
    </div>
  )
}
