import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import BusyNotice from './BusyNotice'

interface ToastItem {
  id: number
  kind: 'success' | 'error' | 'info'
  message: string
  action?: ToastAction
}

// A button inside a toast, for the one thing that fixes what it says.
export interface ToastAction {
  label: string
  onClick: () => void
}

interface ToastApi {
  success: (message: string, action?: ToastAction) => void
  error: (message: string, action?: ToastAction) => void
  info: (message: string, action?: ToastAction) => void
}

const ToastContext = createContext<ToastApi | null>(null)

// A small, unmissable confirmation for controls that save the moment you
// change them (toggles, switches): "Saved" slides in, then goes away.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const nextId = useRef(1)
  const hold = useRef(new Map<number, { kind: ToastItem['kind']; timer: ReturnType<typeof setTimeout> }>())
  const drop = useCallback((id: number) => {
    hold.current.delete(id)
    setItems((cur) => cur.filter((t) => t.id !== id))
  }, [])

  const push = useCallback((kind: ToastItem['kind'], message: string, action?: ToastAction) => {
    const id = nextId.current++
    setItems((cur) => [...cur.slice(-3), { id, kind, message, action }])
    // A toast with a button stays long enough to be read and pressed; a longer sentence stays longer than a short one.
    const reading = Math.min(10000, 2600 + message.length * 45)
    hold.current.set(id, { kind, timer: setTimeout(() => drop(id), action ? 12000 : kind === 'error' ? Math.max(6000, reading) : reading) })
  }, [drop])

  // Pointing at a toast keeps it on screen; it goes shortly after the
  // pointer leaves.
  const pause = useCallback((id: number) => {
    const h = hold.current.get(id)
    if (h) clearTimeout(h.timer)
  }, [])
  const resume = useCallback(
    (id: number) => {
      const h = hold.current.get(id)
      if (h) {
        clearTimeout(h.timer)
        h.timer = setTimeout(() => drop(id), 2000)
      }
    },
    [drop],
  )

  const api = useMemo<ToastApi>(
    () => ({
      success: (m, a) => push('success', m, a),
      error: (m, a) => push('error', m, a),
      info: (m, a) => push('info', m, a),
    }),
    [push],
  )

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="toast-stack" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`} role={t.kind === 'error' ? 'alert' : undefined} onMouseEnter={() => pause(t.id)} onMouseLeave={() => resume(t.id)} onFocus={() => pause(t.id)} onBlur={() => resume(t.id)}>
            <span className="toast-icon" aria-hidden="true">
              {t.kind === 'success' ? '✓' : t.kind === 'error' ? '!' : 'i'}
            </span>
            {t.message}
            {t.action && (
              <button
                type="button"
                className="btn-sm toast-action"
                onClick={() => {
                  const run = t.action?.onClick
                  drop(t.id)
                  run?.()
                }}
              >
                {t.action.label}
              </button>
            )}
          </div>
        ))}
        <BusyNotice />
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside <ToastProvider>')
  return ctx
}
