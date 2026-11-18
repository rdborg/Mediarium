import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api'
import { addStart } from './addDefaults'

// The "Start searching when a title is added" choice from Settings (Finding
// releases > Automatic searching) for an add dialog or a quick Add button.
// It is on until the setting has loaded and stays on if it can't be read, which
// is how adding always behaved. A choice made in the dialog before the setting
// arrives wins over it.
export function useAddSearchNow(): { searchNow: boolean; setSearchNow: (on: boolean) => void } {
  const [searchNow, set] = useState(true)
  const edited = useRef(false)
  useEffect(() => {
    let live = true
    api
      .listProfiles()
      .then((r) => {
        if (live && !edited.current) set(addStart(r).searchNow)
      })
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [])
  const setSearchNow = useCallback((on: boolean) => {
    edited.current = true
    set(on)
  }, [])
  return { searchNow, setSearchNow }
}
