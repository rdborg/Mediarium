import { useEffect, useId, useState, type ReactNode } from 'react'
import { api, type FolderCheck } from '../api'
import { formatBytes } from '../format'
import { composeLine, FOLDER_INFO, isStale, type FolderKind } from '../setupHelpers'
import { FieldError, type Validation } from '../useValidation'
import CopyBox from './CopyBox'

// The folder above a path, or '' at the top.
function parentOf(path: string): string {
  const p = path.trim().replace(/\/+$/, '')
  const i = p.lastIndexOf('/')
  return i > 0 ? p.slice(0, i) : ''
}

interface Status {
  check: FolderCheck
  // For a folder that does not exist yet: whether the folder above it is a
  // real, mapped folder (so Mediarium can create this one when it needs it).
  parentMapped: boolean
}

// One folder box in the setup wizard: the label, the box, a note under it, and
// what Mediarium found at that path. A folder that is not mapped from the
// server gets the exact compose line to add.
export default function FolderBox({
  kind,
  value,
  onChange,
  v,
  mapped,
  inUse,
  parent,
  parentValue,
  optionalNote,
  hint,
  quiet,
}: {
  kind: FolderKind
  value: string
  onChange: (v: string) => void
  v?: Validation
  // The folder the container maps (from the environment), and the folder the
  // app is set to use right now. They differ when an old value was saved.
  mapped: string
  inUse: string
  // A folder that is already mapped and holds the others, for example /data.
  parent: string
  // What to offer inside that parent for this kind, for example /data/Music.
  parentValue: string
  optionalNote?: string
  hint?: ReactNode
  // Folders for things that do not exist yet: no warnings, just the line to add later.
  quiet?: boolean
}) {
  const info = FOLDER_INFO[kind]
  const id = useId()
  const [status, setStatus] = useState<Status | null>(null)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const path = value.trim()

  // Make the folder now (only one level inside a mapped, writable one; the
  // server refuses anything else), then show its real status.
  async function createNow(target: string) {
    setCreating(true)
    setCreateError('')
    try {
      const check = await api.createFolder(target)
      if (target === path) setStatus({ check, parentMapped: true })
      else onChange(target)
    } catch (e) {
      setCreateError(e instanceof Error ? e.message : String(e))
    } finally {
      setCreating(false)
    }
  }

  useEffect(() => {
    if (!path) {
      setStatus(null)
      return
    }
    let stale = false
    const t = setTimeout(async () => {
      try {
        const check = await api.folderCheck(path)
        let parentMapped = false
        if (!check.exists) {
          // The nearest folder above that does exist decides it.
          let up = parentOf(path)
          for (let i = 0; i < 8 && up; i++) {
            const p = await api.folderCheck(up).catch(() => null)
            if (p && p.exists) {
              parentMapped = p.mounted || !p.mountKnown
              break
            }
            up = parentOf(up)
          }
        }
        if (!stale) setStatus({ check, parentMapped })
      } catch {
        if (!stale) setStatus(null)
      }
    }, 400)
    return () => {
      stale = true
      clearTimeout(t)
    }
  }, [path])

  const r = status?.check
  const inDocker = r?.inDocker ?? r?.mountKnown ?? false
  const insideContainer = !!r && r.exists && r.mountKnown && !r.mounted
  const missing = !!r && !r.exists
  const needsLine = inDocker && !quiet && (insideContainer || (missing && !status?.parentMapped))
  const staleNote = isStale(inUse, mapped) && path === mapped.trim()
  const canUseParent = !!parent && needsLine && path !== parentValue

  return (
    <div className="fbox">
      <label htmlFor={id}>
        <span className="fbox-label">
          {info.label}
          {optionalNote && <span className="fbox-tag">{optionalNote}</span>}
        </span>
        <input id={id} value={value} onChange={(e) => onChange(e.target.value)} spellCheck={false} placeholder={info.fallback} {...(v ? v.bind(kind, value, onChange) : {})} />
        {v && <FieldError v={v} name={kind} />}
        {hint && <small className="field-hint">{hint}</small>}
      </label>

      {mapped && mapped !== info.fallback && path !== mapped.trim() && (
        <p className="fbox-note">
          Your compose file sets this to <code>{mapped}</code>.{' '}
          <button type="button" className="link-btn" onClick={() => onChange(mapped)}>
            Use {mapped}
          </button>
        </p>
      )}
      {staleNote && (
        <p className="fbox-note">
          It was set to <code>{inUse}</code> before. Continuing changes it to <code>{mapped}</code>, the folder your compose file maps.{' '}
          <button type="button" className="link-btn" onClick={() => onChange(inUse)}>
            Keep {inUse}
          </button>
        </p>
      )}

      {r && (
        <div className="fbox-status">
          {r.exists && (
            <div className="folder-badges">
              <span className={`badge ${r.writable ? 'downloaded' : 'failed'}`}>{r.writable ? 'writable' : 'read-only'}</span>
              {r.mountKnown && r.mounted && <span className="badge downloaded">Mapped to your device</span>}
              {r.totalBytes > 0 && (
                <span className="badge">
                  {formatBytes(r.freeBytes)} free of {formatBytes(r.totalBytes)}
                </span>
              )}
            </div>
          )}
          {missing && status?.parentMapped && (
            <p className="fbox-calm">
              This folder isn&apos;t there yet. Mediarium creates it when it&apos;s needed, or{' '}
              {r.canCreate ? (
                <button type="button" className="link-btn" disabled={creating} onClick={() => void createNow(path)}>
                  {creating ? 'creating…' : 'create it now'}
                </button>
              ) : (
                'you can create it yourself'
              )}
              .
            </p>
          )}
          {createError && <p className="fbox-warn">{createError}</p>}
          {missing && !inDocker && !quiet && <p className="fbox-warn">This folder doesn&apos;t exist on this computer. Create it, or type a folder that does.</p>}
          {missing && inDocker && quiet && !status?.parentMapped && <p className="fbox-calm">Nothing is mapped here yet, which is fine.</p>}
          {r.exists && !insideContainer && r.warnings.map((w) => <p key={w} className="fbox-warn">{w}</p>)}
        </div>
      )}

      {needsLine && (
        <div className="fbox-fix">
          <strong>{insideContainer ? 'This folder only exists inside the container.' : 'This folder isn’t mapped to your server yet.'}</strong>
          <p>
            {insideContainer ? 'Files saved here are lost when the container is updated. ' : 'Docker can only see the folders you map in your compose file. '}
            Add this line under <code>volumes:</code> and change the first part to the real folder on your server:
          </p>
          <CopyBox text={composeLine(info.hostExample, path || info.fallback)} label={`Compose line for the ${info.label.toLowerCase()}`} />
          <p className="fbox-small">
            Before the colon is the folder on your server (this one is an example). After it is the name Mediarium uses, which has to
            match the box above. Create the folder first, or Docker won&apos;t start.
          </p>
          {canUseParent && (
            <p className="fbox-small">
              Already mapping <code>{parent}</code>? Then skip this and use a folder inside it.{' '}
              <button type="button" className="link-btn" disabled={creating} onClick={() => void createNow(parentValue)}>
                {creating ? 'Creating…' : `Use ${parentValue}`}
              </button>
            </p>
          )}
        </div>
      )}
      {quiet && inDocker && missing && !status?.parentMapped && (
        <details className="fbox-later">
          <summary>How to map it later</summary>
          <p className="fbox-small">When you want it, add this line under volumes: in your compose file (first part is the folder on your server):</p>
          <CopyBox text={composeLine(info.hostExample, path || info.fallback)} label={`Compose line for the ${info.label.toLowerCase()}`} />
        </details>
      )}
    </div>
  )
}
