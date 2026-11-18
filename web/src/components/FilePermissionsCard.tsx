import { useCallback, useEffect, useState } from 'react'
import { api, type FilePermissions } from '../api'
import { DOCS_URL } from '../docs'
import { checkMode, modeFromUmask, symbolic } from '../filePermissions'
import { t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'
import TipLabel from './TipLabel'

// Settings > Library and files > Folders: the permissions given to
// files and folders Mediarium creates in the library. Empty = leave it to the
// container (its UMASK), which is how it has always worked.
export default function FilePermissionsCard() {
  const toast = useToast()
  const [saved, setSaved] = useState<FilePermissions | null>(null)
  const [fileMode, setFileMode] = useState('')
  const [folderMode, setFolderMode] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .getFilePermissions()
      .then((p) => {
        setSaved(p)
        setFileMode(p.fileMode)
        setFolderMode(p.folderMode)
        setConfirm(false)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  const file = checkMode(fileMode, false)
  const folder = checkMode(folderMode, true)
  const world = file.worldWritable || folder.worldWritable
  const changed = saved !== null && (fileMode.trim() !== saved.fileMode || folderMode.trim() !== saved.folderMode)
  const invalid = file.problem !== null || folder.problem !== null
  const umaskFile = saved?.umask ? modeFromUmask(saved.umask, false) : null
  const umaskFolder = saved?.umask ? modeFromUmask(saved.umask, true) : null

  async function save() {
    setBusy(true)
    setError('')
    try {
      const p = await api.putFilePermissions({ fileMode: fileMode.trim(), folderMode: folderMode.trim(), confirmWorldWritable: confirm })
      setSaved(p)
      setFileMode(p.fileMode)
      setFolderMode(p.folderMode)
      toast.success(p.fileMode || p.folderMode ? t('Saved. New files and folders get these permissions.') : t('Saved. New files and folders get the container default.'))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  function preset(f: string, d: string) {
    setFileMode(f)
    setFolderMode(d)
    setConfirm(false)
  }

  return (
    <fieldset className="group span-all">
      <legend>
        <Icon name="shield" size={14} /> {t('File permissions')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('The permissions given to files and folders Mediarium creates in your library: imported files, new title folders, subtitles. Leave both empty to keep what the container gives them. Folders that were already there are never changed.')}{' '}
        <a href={`${DOCS_URL}/library.md#file-permissions`} target="_blank" rel="noreferrer">
          {t('More about permissions')}
        </a>
      </p>
      <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', alignItems: 'start' }}>
        <label style={{ display: 'grid', gap: 4, width: 180 }}>
          <TipLabel text={t('Unix permission number for new files, such as 644 (owner writes, everyone reads). Empty uses the container\'s default.')}>{t('File mode')}</TipLabel>
          <input style={{ width: '100%', boxSizing: 'border-box' }} value={fileMode}onChange={(e) => setFileMode(e.target.value)} placeholder="644" inputMode="numeric" maxLength={5} spellCheck={false} disabled={busy || saved === null} aria-invalid={file.problem !== null} />
          <small className="field-hint">{file.value !== null ? symbolic(file.value) : umaskFile !== null ? t('Container default: {mode}', { mode: symbolic(umaskFile) }) : t('Container default')}</small>
        </label>
        <label style={{ display: 'grid', gap: 4, width: 180 }}>
          <TipLabel text={t('Unix permission number for new folders, such as 755. Folders need the execute digit to be opened. Empty uses the container\'s default.')}>{t('Folder mode')}</TipLabel>
          <input style={{ width: '100%', boxSizing: 'border-box' }} value={folderMode}onChange={(e) => setFolderMode(e.target.value)} placeholder="755" inputMode="numeric" maxLength={5} spellCheck={false} disabled={busy || saved === null} aria-invalid={folder.problem !== null} />
          <small className="field-hint">{folder.value !== null ? symbolic(folder.value) : umaskFolder !== null ? t('Container default: {mode}', { mode: symbolic(umaskFolder) }) : t('Container default')}</small>
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignSelf: 'end', paddingBottom: 22 }}>
          <button className="btn-sm" onClick={() => preset('644', '755')} disabled={busy}>
            {t('Owner writes, others read (644 / 755)')}
          </button>
          <button className="btn-sm" onClick={() => preset('664', '775')} disabled={busy}>
            {t('Group can write too (664 / 775)')}
          </button>
          <button className="btn-sm ghost" onClick={() => preset('', '')} disabled={busy}>
            {t('Container default')}
          </button>
        </div>
      </div>
      {(file.problem || folder.problem) && (
        <p className="error-text" style={{ marginBottom: 0 }}>
          {file.problem ? `${t('File mode')}: ${file.problem}` : `${t('Folder mode')}: ${folder.problem}`}
        </p>
      )}
      {world && !invalid && (
        <div className="notice notice-warn" role="alert" style={{ marginTop: 12 }}>
          <p style={{ margin: '0 0 8px' }}>
            <Icon name="warning" size={14} />{' '}
            {t('This lets every user on the machine, and anything else that can reach these folders, change or delete your files. 664 and 775 are the usual choice when a group needs to write.')}
          </p>
          <label style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)} />
            <span>{t('I understand, save it anyway')}</span>
          </label>
        </div>
      )}
      {saved?.umask && (
        <p className="field-hint" style={{ marginBottom: 0 }}>
          {t('This container was started with UMASK={umask}.', { umask: saved.umask })}
        </p>
      )}
      <div style={{ marginTop: 14 }}>
        <button className="primary" onClick={() => void save()} disabled={busy || !changed || invalid || (world && !confirm)}>
          {t('Save')}
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}
