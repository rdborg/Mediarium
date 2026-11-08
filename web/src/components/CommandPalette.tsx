import { createPortal } from 'react-dom'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, isAdmin } from '../api'
import { useAuth } from '../AuthContext'
import { commandsFor, matching, type Command, type CommandAction } from '../commands'
import { hitCommands } from '../librarySuggest'
import { settingsNavFor } from '../settingsNav'
import { useFocusTrap } from '../useFocusTrap'
import Icon from './Icon'
import { useToast } from './Toast'
import { plural, t } from '../i18n'

// Ctrl+K (Cmd+K on a Mac) anywhere: type a few letters of a page, a settings
// page or something to do, and Enter goes there. Anything else is searched for.
export default function CommandPalette() {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])
  return open ? <Palette onClose={() => setOpen(false)} /> : null
}

function Palette({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const toast = useToast()
  const { user } = useAuth()
  const admin = isAdmin(user)
  const all = useMemo(() => commandsFor(settingsNavFor(admin, user?.role === 'viewer'), admin), [admin, user?.role])
  const [text, setText] = useState('')
  const [at, setAt] = useState(0)
  const box = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLUListElement>(null)
  useFocusTrap(box)

  // The library titles that start with what was typed, from the server's
  // search index. They come a moment after the typing stops.
  const [titles, setTitles] = useState<Command[]>([])
  useEffect(() => {
    const q = text.trim()
    if (!q) {
      setTitles([])
      return
    }
    const ctl = new AbortController()
    const timer = setTimeout(() => {
      api
        .librarySuggest(q, ctl.signal)
        .then((hits) => setTitles(hitCommands(hits)))
        .catch(() => {
          // Titles are a bonus here: the pages and actions still work without them.
          if (!ctl.signal.aborted) setTitles([])
        })
    }, 120)
    return () => {
      clearTimeout(timer)
      ctl.abort()
    }
  }, [text])

  const found = [...matching(all, text), ...(text.trim() ? titles : [])]
  // The last row searches for what was typed, so Enter always does something.
  const rows: Command[] = text.trim() ? [...found, { id: 'search', label: t('Search for “{q}”', { q: text.trim() }), to: `/search?q=${encodeURIComponent(text.trim())}` }] : found
  const pick = Math.min(at, rows.length - 1)

  useEffect(() => {
    list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [pick])

  async function act(action: CommandAction) {
    try {
      if (action === 'pause-all') {
        const r = await api.pauseAllQueue()
        toast.success(r.paused === 0 ? t('Nothing was running.') : plural(r.paused, 'Paused {n} download.', 'Paused {n} downloads.'))
      } else if (action === 'resume-all') {
        const r = await api.resumeAllQueue()
        if (r.skipped > 0) toast.error(`${t('{resumed} resumed, {skipped} could not start.', { resumed: r.resumed, skipped: r.skipped })} ${r.problem ?? ''}`.trim())
        else toast.success(r.resumed === 0 ? t('Nothing was paused.') : plural(r.resumed, 'Resumed {n} download.', 'Resumed {n} downloads.'))
      } else {
        const s = await api.setQuiet(action === 'quiet-hour' ? 60 : 0)
        toast.success(s.active ? t('Quiet for a while. The heavy background work waits.') : t('Back to normal.'))
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  function run(cmd: Command | undefined) {
    if (!cmd) return
    onClose()
    if (cmd.to) navigate(cmd.to)
    else if (cmd.action) void act(cmd.action)
  }

  function onKey(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setAt((pick + 1) % Math.max(rows.length, 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setAt((pick - 1 + rows.length) % Math.max(rows.length, 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      run(rows[pick])
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }

  return createPortal(
    <div className="modal-backdrop palette-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} className="modal palette" role="dialog" aria-modal="true" aria-label={t('Go to')} onKeyDown={onKey}>
        <div className="palette-input">
          <Icon name="search" size={16} />
          <input
            autoFocus
            value={text}
            onChange={(e) => {
              setText(e.target.value)
              setAt(0)
            }}
            placeholder={t('Go to a page, or do something…')}
            aria-label={t('Go to')}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-list"
            aria-activedescendant={rows[pick] ? `palette-${rows[pick].id}` : undefined}
            spellCheck={false}
            autoComplete="off"
          />
          <kbd>Esc</kbd>
        </div>
        <ul ref={list} id="palette-list" className="palette-list" role="listbox" aria-label={t('Go to')}>
          {rows.map((c, i) => (
            <li
              key={c.id}
              id={`palette-${c.id}`}
              role="option"
              aria-selected={i === pick}
              className={i === pick ? 'on' : ''}
              onMouseMove={() => i !== pick && setAt(i)}
              onMouseDown={(e) => {
                e.preventDefault()
                run(c)
              }}
            >
              <Icon name={c.action ? 'play' : c.id === 'search' ? 'search' : 'chevron-right'} size={14} />
              <span className="palette-label">{c.label}</span>
              {c.hint && <span className="palette-hint">{c.hint}</span>}
            </li>
          ))}
          {rows.length === 0 && <li className="palette-empty">{t('Nothing by that name.')}</li>}
        </ul>
      </div>
    </div>,
    document.body,
  )
}
