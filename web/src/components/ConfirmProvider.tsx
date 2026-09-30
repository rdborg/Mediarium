import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react'
import ConfirmDialog from './ConfirmDialog'

export interface ConfirmOptions {
  title: string
  body?: ReactNode
  confirmLabel: string
  cancelLabel?: string
  danger?: boolean // red confirm button, for removing or deleting things
  // An extra choice shown as a tick box, for example "Also delete the files".
  // `warning` appears in red while the box is ticked.
  option?: { label: ReactNode; hint?: ReactNode; defaultChecked?: boolean; warning?: ReactNode }
}

// null when the question was cancelled; otherwise whether the option was ticked.
export type ConfirmResult = { checked: boolean } | null

const ConfirmContext = createContext<((o: ConfirmOptions) => Promise<ConfirmResult>) | null>(null)

// Asks a question in the app's own dialog instead of the browser's confirm
// box: `if (!(await confirm({...}))) return`.
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<ConfirmOptions | null>(null)
  const [checked, setChecked] = useState(false)
  const resolver = useRef<((r: ConfirmResult) => void) | null>(null)

  const confirm = useCallback((o: ConfirmOptions) => {
    resolver.current?.(null)
    setChecked(!!o.option?.defaultChecked)
    setOpen(o)
    return new Promise<ConfirmResult>((resolve) => {
      resolver.current = resolve
    })
  }, [])

  function finish(r: ConfirmResult) {
    resolver.current?.(r)
    resolver.current = null
    setOpen(null)
  }

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      {open && (
        <ConfirmDialog
          title={open.title}
          confirmLabel={open.confirmLabel}
          cancelLabel={open.cancelLabel}
          danger={open.danger}
          onConfirm={() => finish({ checked })}
          onCancel={() => finish(null)}
        >
          {open.body}
          {open.option && (
            <label className="confirm-option">
              <input type="checkbox" checked={checked} onChange={(e) => setChecked(e.target.checked)} />
              <span>
                {open.option.label}
                {open.option.hint && <small>{open.option.hint}</small>}
              </span>
            </label>
          )}
          {open.option?.warning && checked && (
            <p className="confirm-warning" role="alert">
              {open.option.warning}
            </p>
          )}
        </ConfirmDialog>
      )}
    </ConfirmContext.Provider>
  )
}

export function useConfirm() {
  const ctx = useContext(ConfirmContext)
  if (!ctx) throw new Error('useConfirm must be used within ConfirmProvider')
  return ctx
}
