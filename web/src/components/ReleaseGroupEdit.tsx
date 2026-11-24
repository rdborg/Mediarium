import { useState } from 'react'
import { t } from '../i18n'

// The release group written into a file's name, with a way to change it by
// hand when the group Mediarium found is wrong or missing. Saving renames the
// file (unless files keep their original names).
export default function ReleaseGroupEdit({ group, save, compact = false }: { group: string; save: (group: string) => Promise<void>; compact?: boolean }) {
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState(group)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit() {
    setBusy(true)
    setError('')
    try {
      await save(value.trim())
      setEditing(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!editing) {
    return (
      <span className={`release-group${compact ? ' compact' : ''}`}>
        <span className="rg-label">{t('Release group')}</span>
        <span className="rg-value">{group || t('none')}</span>
        <button
          type="button"
          className="btn-sm"
          onClick={() => {
            setValue(group)
            setError('')
            setEditing(true)
          }}
          title={t('Change the release group written into this file’s name')}
        >
          {t('Edit')}
        </button>
      </span>
    )
  }
  return (
    <form
      className={`release-group editing${compact ? ' compact' : ''}`}
      onSubmit={(ev) => {
        ev.preventDefault()
        void submit()
      }}
    >
      <label className="rg-label" htmlFor="rg-input">
        {t('Release group')}
      </label>
      <input id="rg-input" value={value} onChange={(ev) => setValue(ev.target.value)} maxLength={40} autoFocus aria-describedby={error ? 'rg-error' : undefined} placeholder={t('none')} disabled={busy} />
      <button type="submit" className="btn-sm primary" disabled={busy}>
        {busy ? t('Saving…') : t('Save')}
      </button>
      <button type="button" className="btn-sm" onClick={() => setEditing(false)} disabled={busy}>
        {t('Cancel')}
      </button>
      {error && (
        <span id="rg-error" className="rg-error" role="alert">
          {error}
        </span>
      )}
    </form>
  )
}
