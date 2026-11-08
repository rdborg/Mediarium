import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { REPO_URL } from '../docs'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import { notesToShow, SEEN_KEY, titleVersion, type ReleaseNotes } from '../whatsNew'
import Icon from './Icon'
import FeatureOffers from './FeatureOffers'

function readSeen(): string | null {
  try {
    return localStorage.getItem(SEEN_KEY)
  } catch {
    return null
  }
}

function writeSeen(v: string) {
  try {
    localStorage.setItem(SEEN_KEY, v)
  } catch {
    // Private windows may refuse; the note then shows again next time.
  }
}

// After Mediarium updates, a short note with the new version's highlights,
// once in each browser. A browser that has never seen Mediarium before only
// remembers the version, so a fresh install doesn't greet anyone with it.
// onDone runs once there is nothing (more) to show, so another window can wait
// for this one instead of opening on top of it.
export default function WhatsNew({ version, onDone }: { version: string; onDone?: () => void }) {
  const [notes, setNotes] = useState<ReleaseNotes[]>([])
  const [lastSeen, setLastSeen] = useState<string | null>(null)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!version) return
    const seen = readSeen()
    if (seen === null) {
      writeSeen(version)
      onDone?.()
      return
    }
    const due = notesToShow(seen, version)
    if (due.length > 0) {
      setLastSeen(seen)
      setNotes(due)
      return
    }
    if (seen !== version) writeSeen(version)
    onDone?.()
  }, [version, onDone])

  const close = () => {
    writeSeen(version)
    setNotes([])
    onDone?.()
  }

  useEffect(() => {
    if (notes.length === 0) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  if (notes.length === 0) return null
  return <WhatsNewDialog notes={notes} version={version} lastSeen={lastSeen} onClose={close} box={box} />
}

function WhatsNewDialog({ notes, version, lastSeen, onClose, box }: { notes: ReleaseNotes[]; version: string; lastSeen: string | null; onClose: () => void; box: React.RefObject<HTMLDivElement | null> }) {
  // Start at the top of the notes: focusing the last button would scroll a long list to its end on a phone.
  useEffect(() => {
    box.current?.focus({ preventScroll: true })
    box.current?.scrollTo?.(0, 0)
  }, [box])
  useFocusTrap(box)
  const newest = titleVersion(version)
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} tabIndex={-1} className="modal whats-new" role="dialog" aria-modal="true" aria-labelledby="whats-new-title">
        <div className="confirm-head">
          <span className="confirm-ico">
            <Icon name="sparkles" size={20} />
          </span>
          <h2 id="whats-new-title">{t("What's new in Mediarium {version}", { version: newest })}</h2>
        </div>
        <div className="confirm-body">
          {notes.map((n) => (
            <section key={n.version}>
              {notes.length > 1 && <h3>{n.version}</h3>}
              <ul>
                {n.items.map((item) => (
                  <li key={item}>{item}</li>
                ))}
              </ul>
            </section>
          ))}
          <FeatureOffers mode="update" version={version} lastSeen={lastSeen} onOpen={onClose} />
          <p className="hint">
            <a href={`${REPO_URL}/blob/main/CHANGELOG.md`} target="_blank" rel="noreferrer">
              {t('See every change')}
            </a>
          </p>
        </div>
        <div className="modal-foot">
          <button className="primary" onClick={onClose}>
            {t('Got it')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
