import { useEffect, useState } from 'react'
import { api } from '../api'
import FolderStatus from './FolderStatus'
import Icon from './Icon'
import { useToast } from './Toast'
import { t } from '../i18n'
import { keptRule, ruleChoices } from '../newTitleFolder'

// Settings > Library and files > Folders: more movie or TV folders next
// to the main one, for another drive or a folder kept apart (kids, concerts,
// documentaries). When there are some, adding a title asks which folder to
// keep it in, and a title's page can move it to another for new files. Under
// the list, "New movies go to" is the folder picked when nothing is chosen:
// the one the Add dialog starts on, and the one for titles that arrive from
// a list, a request, a followed collection or another app. It can be the
// main folder, one of the extra ones, or whichever has the most room (two
// drives filling up side by side). Saved with its own button.
export default function ExtraFolders({ kind }: { kind: 'movies' | 'tv' }) {
  const toast = useToast()
  const key = kind === 'movies' ? 'moviesExtraPaths' : 'tvExtraPaths'
  const ruleKey = kind === 'movies' ? 'moviesNewFolder' : 'tvNewFolder'
  const [saved, setSaved] = useState<{ list: string[]; rule: string } | null>(null)
  const [main, setMain] = useState('')
  const [rows, setRows] = useState<string[]>([])
  const [rule, setRule] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        const list = (s[key] as string[] | undefined) ?? []
        const r = (s[ruleKey] as string | undefined) ?? ''
        setSaved({ list, rule: r })
        setRows(list)
        setRule(r)
        setMain((kind === 'movies' ? s.moviesPath : s.tvPath) ?? '')
      })
      .catch(() => setSaved({ list: [], rule: '' }))
  }, [key, ruleKey, kind])

  const cleaned = rows.map((r) => r.trim()).filter(Boolean)
  const effectiveRule = keptRule(rule, cleaned)
  const changed = saved !== null && (cleaned.join('\n') !== saved.list.join('\n') || effectiveRule !== saved.rule)

  async function save() {
    setBusy(true)
    setError('')
    try {
      const s = await api.putSettings({ [key]: cleaned, [ruleKey]: effectiveRule })
      const list = (s[key] as string[] | undefined) ?? cleaned
      const r = (s[ruleKey] as string | undefined) ?? effectiveRule
      setSaved({ list, rule: r })
      setRows(list)
      setRule(r)
      toast.success(list.length === 0 ? (kind === 'movies' ? t('Saved: only the main movies folder is used.') : t('Saved: only the main TV folder is used.')) : kind === 'movies' ? t('Saved: movies can also go in {n} more folders.', { n: list.length }) : t('Saved: shows can also go in {n} more folders.', { n: list.length }))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="path-col">
      <strong>{kind === 'movies' ? t('More movie folders') : t('More TV folders')}</strong>
      {rows.map((row, i) => (
        <div key={i} className="extra-folder">
          <div className="extra-folder-row">
            <input
              value={row}
              placeholder={kind === 'movies' ? '/movies-kids' : '/tv-kids'}
              spellCheck={false}
              aria-label={kind === 'movies' ? t('A movie folder') : t('A TV folder')}
              onChange={(e) => setRows((r) => r.map((x, j) => (j === i ? e.target.value : x)))}
            />
            <button className="icon-btn" onClick={() => setRows((r) => r.filter((_, j) => j !== i))} aria-label={t('Remove this folder from the list')} title={t('Remove this folder from the list')}>
              <Icon name="trash" size={15} />
            </button>
          </div>
          {row.trim() && <FolderStatus path={row.trim()} />}
        </div>
      ))}
      {cleaned.length > 0 && (
        <label>
          {kind === 'movies' ? t('New movies go to') : t('New shows go to')}
          <select value={effectiveRule} onChange={(e) => setRule(e.target.value)} aria-label={kind === 'movies' ? t('New movies go to') : t('New shows go to')}>
            {ruleChoices(main, cleaned).map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <small className="path-note">{t('The folder picked when nothing is chosen: the one the Add dialog starts on, and the one for titles that come from a list, a request, a followed collection or another app. "The folder with the most room" looks at the free space each time a title is added.')}</small>
        </label>
      )}
      <div className="row-actions">
        <button className="btn-sm btn-with-icon" onClick={() => setRows((r) => [...r, ''])}>
          <Icon name="plus" size={14} /> {t('Add a folder')}
        </button>
        {changed && (
          <button className="btn-sm primary" disabled={busy} onClick={() => void save()}>
            {busy ? t('Saving…') : t('Save folders')}
          </button>
        )}
      </div>
      {error && <p className="field-error" role="alert">{error}</p>}
      <small className="path-note">{kind === 'movies' ? t('For example /movies-kids or /mnt/disk2/movies. When you add a movie you choose which folder it goes in.') : t('For example /tv-kids or /mnt/disk2/tv. When you add a show you choose which folder it goes in.')}</small>
    </div>
  )
}
