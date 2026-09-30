import { useState, type ReactNode } from 'react'
import Icon from './Icon'
import type { SelectionText } from '../librarySelection'

// The bar that stays at the top of the Library while you pick titles: how
// many are picked, "Select all in this view" and "Select none", and the
// things you can do to all of them (passed in as children).
export default function BulkBar({
  text,
  onSelectAll,
  onSelectNone,
  onDone,
  busy,
  children,
}: {
  text: SelectionText
  onSelectAll: () => void
  onSelectNone: () => void
  onDone: () => void
  busy: boolean
  children: ReactNode
}) {
  // On a phone the actions fold away behind one button, so the bar that stays
  // in view takes one row, not half the screen.
  const [open, setOpen] = useState(false)
  return (
    <div className={`bulkbar${open ? ' open' : ''}`} role="toolbar" aria-label="Actions for the selected titles">
      <div className="bulkbar-head">
        <strong aria-live="polite">{text.count}</strong>
        <button className="btn-sm" onClick={onSelectAll} disabled={busy || !text.selectAll || text.allSelected}>
          {text.selectAll || 'Select all in this view'}
        </button>
        <button className="btn-sm" onClick={onSelectNone} disabled={busy || text.count === 'Nothing selected'}>
          Select none
        </button>
        {text.scope && <span className="bulkbar-scope">{text.scope}</span>}
        <button className="btn-sm bulkbar-toggle btn-with-icon" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          Actions <Icon name={open ? 'chevron-up' : 'chevron-down'} size={14} />
        </button>
        <button className="btn-sm bulkbar-done" onClick={onDone}>
          Done
        </button>
      </div>
      <div className="bulkbar-actions">{children}</div>
    </div>
  )
}
