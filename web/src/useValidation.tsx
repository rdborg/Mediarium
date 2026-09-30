import { useId, useState, type FocusEvent } from 'react'
import type { Message } from './validate'

// Inline field errors for a form.
//
//   const errors = { email: firstError(required(email, '...'), validate.email(email)) }
//   const v = useValidation(errors)
//   ...
//   <label>
//     Email
//     <input value={email} onChange={...} {...v.bind('email', email, setEmail)} />
//     <FieldError v={v} name="email" />
//   </label>
//   ...
//   function save() { if (!v.attempt()) return; ... }
//   <FormProblem v={v} />
//
// A field's message appears once the person has left the field, or tried to
// submit. Until then nothing is shown, so nobody is told off for a half-typed
// value. `bind` also trims stray spaces when the field loses focus.

// A message that appears the moment a field loses focus can push a button
// down before the mouse is released, so the click never lands. While a mouse
// button or finger is down, showing the message waits until it is released
// (after the click has been delivered).
let pointerIsDown = false
const afterRelease: (() => void)[] = []
if (typeof document !== 'undefined') {
  const down = () => {
    pointerIsDown = true
  }
  const up = () =>
    setTimeout(() => {
      pointerIsDown = false
      afterRelease.splice(0).forEach((fn) => fn())
    }, 0)
  document.addEventListener('pointerdown', down, true)
  document.addEventListener('pointerup', up, true)
  document.addEventListener('pointercancel', up, true)
}
function whenPointerUp(fn: () => void) {
  if (pointerIsDown) afterRelease.push(fn)
  else fn()
}

export type Errors = Record<string, Message | undefined>

export interface Validation {
  /** The message to show for a field right now, or null. */
  error: (name: string) => Message
  /** True when every field passes (whether or not it has been touched). */
  ok: boolean
  /** The first problem in the form, in the order the fields were listed. */
  first: Message
  /** True once submit has been tried. */
  attempted: boolean
  /** Call from the submit handler: reveals all errors, focuses the first bad field, returns true when all is well. */
  attempt: () => boolean
  /** Forget touched/attempted state, e.g. after a successful save. */
  reset: () => void
  /** Spread onto the input, textarea or select. Pass a setter to trim the value on blur. */
  bind: (name: string, value?: unknown, setValue?: (v: string) => void) => FieldProps
  /** Id of the message element for a field (used by FieldError). */
  messageId: (name: string) => string
}

export interface FieldProps {
  onBlur: (e: FocusEvent) => void
  'aria-invalid': boolean | undefined
  'aria-describedby': string | undefined
  'data-vf': string
}

export function useValidation(errors: Errors): Validation {
  const formId = useId()
  const [touched, setTouched] = useState<Record<string, boolean>>({})
  const [attempted, setAttempted] = useState(false)

  const messageId = (name: string) => `${formId}-${name}-err`
  const error = (name: string): Message => (attempted || touched[name] ? (errors[name] ?? null) : null)
  const names = Object.keys(errors)
  const first = names.map((n) => errors[n]).find((m) => !!m) ?? null

  return {
    error,
    ok: first === null,
    first,
    attempted,
    attempt() {
      setAttempted(true)
      if (first === null) return true
      // Wait for the errors to render, then move to the first bad field.
      requestAnimationFrame(() => {
        const el = Array.from(document.querySelectorAll<HTMLElement>(`[data-vf="${CSS.escape(formId)}"]`)).find((e) => e.getAttribute('aria-invalid') === 'true')
        el?.focus()
      })
      return false
    },
    reset() {
      setTouched({})
      setAttempted(false)
    },
    bind(name, value, setValue) {
      const msg = error(name)
      return {
        onBlur: () => {
          if (typeof value === 'string' && setValue && value !== value.trim()) setValue(value.trim())
          whenPointerUp(() => setTouched((t) => (t[name] ? t : { ...t, [name]: true })))
        },
        'aria-invalid': msg ? true : undefined,
        'aria-describedby': msg ? messageId(name) : undefined,
        'data-vf': formId,
      }
    },
    messageId,
  }
}

/** The red message under a field. Renders nothing while the field is fine (or untouched). */
export function FieldError({ v, name }: { v: Validation; name: string }) {
  const msg = v.error(name)
  if (!msg) return null
  return (
    <small id={v.messageId(name)} className="field-error" role="alert">
      {msg}
    </small>
  )
}

/** A line under the submit button after a blocked attempt, so it is clear why nothing happened. */
export function FormProblem({ v, verb = 'save' }: { v: Validation; verb?: string }) {
  if (!v.attempted || v.ok) return null
  return (
    <p className="form-problem" role="alert">
      A field above needs fixing before you can {verb}. It is marked in red.
    </p>
  )
}
