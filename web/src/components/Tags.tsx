import { useEffect, useId, useRef, useState } from 'react'
import { api } from '../api'
import Icon from './Icon'
import { useToast } from './Toast'

// Tags on a movie or show ("Kids", "4K"): the person's own words for sorting
// a library that lives in one folder. They filter the Library and become
// collections on Plex, Jellyfin and Emby.

// TagInput is a box that adds a tag on Enter or comma, suggesting the tags
// already in use.
export function TagInput({ suggestions, onAdd, placeholder = 'Add a tag', disabled }: { suggestions: string[]; onAdd: (tag: string) => void; placeholder?: string; disabled?: boolean }) {
  const [text, setText] = useState('')
  const list = useId()
  function commit() {
    const t = text.replace(/,/g, ' ').trim()
    if (t) onAdd(t)
    setText('')
  }
  return (
    <>
      <input
        className="tag-input"
        list={list}
        value={text}
        disabled={disabled}
        placeholder={placeholder}
        aria-label={placeholder}
        maxLength={30}
        onChange={(e) => {
          const v = e.target.value
          // Picking a suggestion fills the whole word: add it straight away.
          if (suggestions.includes(v)) {
            onAdd(v)
            setText('')
            return
          }
          if (v.endsWith(',')) {
            setText(v)
            const t = v.slice(0, -1).trim()
            if (t) onAdd(t)
            setText('')
            return
          }
          setText(v)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            commit()
          }
        }}
        onBlur={commit}
      />
      <datalist id={list}>
        {suggestions.map((s) => (
          <option key={s} value={s} />
        ))}
      </datalist>
    </>
  )
}

export function TagChips({ tags, onRemove }: { tags: string[]; onRemove?: (tag: string) => void }) {
  if (tags.length === 0) return null
  return (
    <span className="tag-chips">
      {tags.map((t) => (
        <span key={t} className="tag-chip">
          <Icon name="bookmark" size={11} /> {t}
          {onRemove && (
            <button type="button" onClick={() => onRemove(t)} aria-label={`Remove the tag ${t}`} title={`Remove ${t}`}>
              ×
            </button>
          )}
        </span>
      ))}
    </span>
  )
}

// TitleTags is the tag editor on a movie's or show's page.
export default function TitleTags({ kind, id }: { kind: 'movie' | 'tv'; id: number }) {
  const toast = useToast()
  const [tags, setTags] = useState<string[] | null>(null)
  const [all, setAll] = useState<string[]>([])
  const saving = useRef(false)

  useEffect(() => {
    api.titleTags(kind, id).then(setTags).catch(() => setTags([]))
    api
      .listTags()
      .then((l) => setAll(l.map((t) => t.name)))
      .catch(() => undefined)
  }, [kind, id])

  async function save(next: string[]) {
    if (saving.current) return
    saving.current = true
    const before = tags
    setTags(next)
    try {
      const r = await api.setTitleTags(kind, id, next)
      setTags(r.tags)
      setAll((cur) => [...new Set([...cur, ...r.tags])].sort((a, b) => a.localeCompare(b)))
    } catch (e) {
      setTags(before)
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      saving.current = false
    }
  }

  if (tags === null) return null
  const lower = new Set(tags.map((t) => t.toLowerCase()))
  return (
    <div className="title-tags">
      <span className="title-tags-label" title="Your own words to sort and filter your library. They also show up as collections on Plex, Jellyfin and Emby.">
        Tags
      </span>
      <TagChips tags={tags} onRemove={(t) => void save(tags.filter((x) => x !== t))} />
      <TagInput suggestions={all.filter((t) => !lower.has(t.toLowerCase()))} onAdd={(t) => !lower.has(t.toLowerCase()) && void save([...tags, t])} placeholder={tags.length === 0 ? 'Add a tag, like Kids or 4K' : 'Add a tag'} />
    </div>
  )
}

// BulkTagMenu adds or removes a tag on every chosen title (Library select mode).
export function BulkTagMenu({ disabled, inSelection, onAdd, onRemove }: { disabled: boolean; inSelection: string[]; onAdd: (tag: string) => void; onRemove: (tag: string) => void }) {
  const [open, setOpen] = useState(false)
  const [all, setAll] = useState<string[]>([])
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    api
      .listTags()
      .then((l) => setAll(l.map((t) => t.name)))
      .catch(() => undefined)
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])
  return (
    <div className="bulk-tags" ref={root}>
      <button className="btn-sm btn-with-icon" disabled={disabled} aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <Icon name="bookmark" size={15} /> Tags
      </button>
      {open && (
        <div className="bulk-tags-pop" role="dialog" aria-label="Tags for the chosen titles">
          <strong>Add a tag to the chosen titles</strong>
          <TagInput
            suggestions={all}
            onAdd={(t) => {
              onAdd(t)
              setOpen(false)
            }}
          />
          {inSelection.length > 0 && (
            <>
              <strong>Remove a tag from them</strong>
              <TagChips
                tags={inSelection}
                onRemove={(t) => {
                  onRemove(t)
                  setOpen(false)
                }}
              />
            </>
          )}
        </div>
      )}
    </div>
  )
}
