import { useState } from 'react'
import { api, type RenameItem } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'

// Settings > Library: rename the files already in the library so they match
// the naming preset. A preview first; only ticked files are renamed.
export default function RenameCard() {
  const toast = useToast()
  const [plan, setPlan] = useState<RenameItem[] | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const key = (i: RenameItem) => `${i.kind}-${i.id}`

  async function preview() {
    setBusy(true)
    try {
      const p = await api.renamePreview()
      setPlan(p)
      setPicked(new Set(p.map(key)))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function run() {
    if (!plan) return
    setBusy(true)
    try {
      const r = await api.rename(plan.filter((i) => picked.has(key(i))).map((i) => ({ kind: i.kind, id: i.id })))
      if (r.failed.length === 0) toast.success(`Renamed ${r.renamed} ${r.renamed === 1 ? 'file' : 'files'}.`)
      else toast.error(`Renamed ${r.renamed}. ${r.failed.length} could not be renamed: ${r.failed.slice(0, 3).join('; ')}`)
      await preview()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const name = (p: string) => p.split(/[\\/]/).slice(-2).join('/')
  return (
    <fieldset className="group span-all">
      <legend>
        <Icon name="refresh" size={14} /> Rename existing files
      </legend>
      <p style={{ marginTop: 0 }}>
        Files keep the name they had when they arrived. After changing the naming preset, this renames them to match. You see every change first, and subtitles move with their video. Your media server is told afterwards.
      </p>
      {plan === null ? (
        <button onClick={() => void preview()} disabled={busy}>
          {busy ? 'Looking…' : 'Show what would change'}
        </button>
      ) : plan.length === 0 ? (
        <p className="field-hint">Every file already has the right name.</p>
      ) : (
        <>
          <div className="toolbar">
            <span>
              {picked.size} of {plan.length} ticked
            </span>
            <button className="btn-sm" onClick={() => setPicked(picked.size === plan.length ? new Set() : new Set(plan.map(key)))}>
              {picked.size === plan.length ? 'Untick all' : 'Tick all'}
            </button>
            <span className="spacer" />
            <button className="primary" disabled={busy || picked.size === 0} onClick={() => void run()}>
              {busy ? 'Renaming…' : `Rename ${picked.size} ${picked.size === 1 ? 'file' : 'files'}`}
            </button>
          </div>
          <div className="rename-list">
            {plan.map((i) => (
              <label key={key(i)} className="rename-row">
                <input
                  type="checkbox"
                  checked={picked.has(key(i))}
                  onChange={() =>
                    setPicked((s) => {
                      const n = new Set(s)
                      if (n.has(key(i))) n.delete(key(i))
                      else n.add(key(i))
                      return n
                    })
                  }
                />
                <span>
                  <strong>{i.title}</strong>
                  <small>
                    {name(i.from)} → {name(i.to)}
                  </small>
                </span>
              </label>
            ))}
          </div>
        </>
      )}
    </fieldset>
  )
}
