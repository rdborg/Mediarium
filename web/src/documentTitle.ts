import { useEffect, useSyncExternalStore } from 'react'

// The browser tab says which page you are on ("Library · Mediarium"), so tabs,
// history and bookmarks are told apart. The app shell sets it from the page
// name; a page that knows more (a movie's name) offers it with
// useDocumentTitle and the shell uses it instead.

export const APP_NAME = 'Mediarium'

// "Library · Mediarium". An empty name is just the app name.
export function titleFor(name: string): string {
  const n = name.trim()
  return n && n !== APP_NAME ? `${n} · ${APP_NAME}` : APP_NAME
}

let specific = ''
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((l) => l())

// A page calls this with what should be in the tab name, for example the title
// of the movie it shows. Leave it empty until that is known.
export function useDocumentTitle(name: string | undefined) {
  useEffect(() => {
    specific = name?.trim() ?? ''
    emit()
    return () => {
      specific = ''
      emit()
    }
  }, [name])
}

// What the shell reads: the page's own name, or '' when it gave none.
export function useSpecificTitle(): string {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => specific,
  )
}
