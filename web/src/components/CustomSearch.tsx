import { useState, type FormEvent } from 'react'
import { t } from '../i18n'

// The box under the releases of a title for looking with other words, for a
// release posted under another name. Leaving it empty brings back the usual list.
export default function CustomSearch({ busy, onSearch }: { busy: boolean; onSearch: (q: string) => void }) {
  const [q, setQ] = useState('')
  function submit(e: FormEvent) {
    e.preventDefault()
    onSearch(q.trim())
  }
  return (
    <form className="custom-search" onSubmit={submit}>
      <input type="search" value={q} maxLength={200} placeholder={t('Search for other words')} aria-label={t('Search for other words')} onChange={(e) => setQ(e.target.value)} />
      <button type="submit" className="btn-sm" disabled={busy}>
        {t('Search')}
      </button>
      <small>{t('Every answer is shown, and the download still goes to this title.')}</small>
    </form>
  )
}
