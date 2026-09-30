import { createPortal } from 'react-dom'
import { useEffect, useRef, type ReactNode } from 'react'
import { useFocusTrap } from '../useFocusTrap'
import Icon from './Icon'

// A small in-app question with two answers, used instead of the browser's
// own confirm box so it looks like the rest of Mediarium.
export default function ConfirmDialog({
  title,
  children,
  confirmLabel,
  cancelLabel = 'Go back',
  danger = false,
  onConfirm,
  onCancel,
}: {
  title: string
  children: ReactNode
  confirmLabel: string
  cancelLabel?: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onCancel()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onCancel])

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onCancel()}>
      <div ref={box} tabIndex={-1} className={`modal confirm-dialog${danger ? ' is-danger' : ''}`} role="alertdialog" aria-modal="true" aria-label={title}>
        <div className="confirm-head">
          <span className="confirm-ico">
            <Icon name={danger ? 'trash' : 'warning'} size={20} />
          </span>
          <h2>{title}</h2>
        </div>
        <div className="confirm-body">{children}</div>
        <div className="modal-foot">
          <button onClick={onCancel} autoFocus>
            {cancelLabel}
          </button>
          <button className={danger ? 'btn-danger-solid' : 'primary'} onClick={onConfirm}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
