import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { api, type OtherItem } from '../api'
import { formatBytes } from '../format'
import { t } from '../i18n'
import { looseIncludes } from '../looseText'
import { useConfirm } from './ConfirmProvider'
import { describeState } from './state'
import Icon from './Icon'
import { useToast } from './Toast'

// The Other tab of the Library: things that are not movies, shows, music or
// books. An item is made by name (nothing is looked up anywhere), or from an
// NZB or torrent, and keeps its files under their own names in a folder named
// after it.
export default function OtherLibrary({ switcher, admin }: { switcher: ReactNode; admin: boolean }) {
  const [items, setItems] = useState<OtherItem[] | null>(null)
  const [root, setRoot] = useState('')
  const [error, setError] = useState('')
  const [text, setText] = useState('')
  const [adding, setAdding] = useState(false)

  const load = useCallback(() => {
    api
      .listOther()
      .then((r) => {
        setItems(r.items)
        setRoot(r.root)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(() => {
    load()
  }, [load])
  // While a download is running the list keeps itself fresh.
  const busy = !!items?.some((i) => i.active)
  useEffect(() => {
    if (!busy) return
    const timer = window.setInterval(load, 4000)
    return () => window.clearInterval(timer)
  }, [busy, load])

  const shown = useMemo(() => (items ?? []).filter((i) => looseIncludes(i.name, text)), [items, text])

  return (
    <div>
      <div className="toolbar lib-toolbar">
        {switcher}
        <input type="search" placeholder={t('Find in your other items…')} value={text} onChange={(e) => setText(e.target.value)} aria-label={t('Find in your library')} />
        {admin && (
          <button className="primary btn-with-icon toolbar-add" onClick={() => setAdding((a) => !a)} aria-expanded={adding}>
            <Icon name={adding ? 'x' : 'plus'} size={16} /> {adding ? t('Close') : t('Add')}
          </button>
        )}
      </div>

      {adding && admin && (
        <AddOther
          onAdded={() => {
            setAdding(false)
            load()
          }}
        />
      )}

      {error && <p className="error" role="alert">{error}</p>}
      {items && items.length === 0 && !adding && (
        <div className="empty-state">
          <p>{t('Nothing here yet.')}</p>
          {root && <p className="field-hint">{t('Add an item by name, or from an NZB or torrent. Its files are kept in {folder}.', { folder: root })}</p>}
        </div>
      )}
      {items && items.length > 0 && (
        <ul className="other-list">
          {shown.map((i) => (
            <OtherRow key={i.id} item={i} admin={admin} onChanged={load} />
          ))}
        </ul>
      )}
      {items && items.length > 0 && root && <p className="field-hint other-root">{t('Kept in {folder}', { folder: root })}</p>}
      {!items && !error && <p className="field-hint">{t('Loading')}</p>}
      <span className="sr-only" aria-live="polite">{items ? t('{count} items', { count: String(shown.length) }) : ''}</span>
    </div>
  )
}

// Adds an item: by name alone, or from a download (the item is then named
// after what is typed, or after the release).
function AddOther({ onAdded }: { onAdded: () => void }) {
  const toast = useToast()
  const [mode, setMode] = useState<'name' | 'download'>('name')
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [working, setWorking] = useState(false)
  const [error, setError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (mode === 'name' && !name.trim()) return setError(t('Give the item a name.'))
    if (mode === 'download' && !file && !url.trim()) return setError(t('Choose an NZB or .torrent file, or paste an address or magnet link.'))
    setWorking(true)
    try {
      if (mode === 'name') {
        await api.addOther(name.trim())
        toast.success(t('{title} added.', { title: name.trim() }))
      } else {
        const form = new FormData()
        form.set('other', 'new')
        if (name.trim()) form.set('otherName', name.trim())
        if (file) form.set('file', file)
        else form.set('url', url.trim())
        const r = await api.manualDownload(form)
        toast.success(t('Downloading {title}.', { title: r.releaseTitle }))
      }
      onAdded()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setWorking(false)
    }
  }

  return (
    <form className="other-add" onSubmit={submit}>
      <div className="seg" role="group" aria-label={t('How to add it')}>
        <button type="button" aria-pressed={mode === 'name'} className={mode === 'name' ? 'active' : ''} onClick={() => setMode('name')}>
          {t('By name')}
        </button>
        <button type="button" aria-pressed={mode === 'download'} className={mode === 'download' ? 'active' : ''} onClick={() => setMode('download')}>
          {t('From a download')}
        </button>
      </div>
      <label>
        {mode === 'name' ? t('Name') : t('Name (optional, the release name is used when empty)')}
        <input value={name} onChange={(e) => setName(e.target.value)} maxLength={200} placeholder={t('Lecture series 1998')} autoFocus />
      </label>
      {mode === 'download' && <DownloadFields file={file} setFile={setFile} url={url} setUrl={setUrl} fileInput={fileInput} />}
      {error && <p className="error" role="alert">{error}</p>}
      <div className="form-actions">
        <button className="primary" type="submit" disabled={working}>
          {working ? t('Adding…') : mode === 'name' ? t('Add item') : t('Add and download')}
        </button>
      </div>
    </form>
  )
}

function DownloadFields({ file, setFile, url, setUrl, fileInput }: { file: File | null; setFile: (f: File | null) => void; url: string; setUrl: (u: string) => void; fileInput: React.RefObject<HTMLInputElement | null> }) {
  return (
    <>
      <label>
        {t('NZB or .torrent file')}
        <input ref={fileInput} type="file" accept=".nzb,.torrent,application/x-bittorrent,application/x-nzb" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
      </label>
      <label>
        {t('Or an address or magnet link')}
        <input value={url} onChange={(e) => setUrl(e.target.value)} disabled={!!file} placeholder="magnet:?xt=urn:btih:…" spellCheck={false} />
      </label>
    </>
  )
}

function OtherRow({ item, admin, onChanged }: { item: OtherItem; admin: boolean; onChanged: () => void }) {
  const toast = useToast()
  const confirm = useConfirm()
  const [open, setOpen] = useState(false)
  const [panel, setPanel] = useState<'' | 'download' | 'import' | 'rename'>('')
  const [detail, setDetail] = useState<OtherItem | null>(null)
  const [name, setName] = useState(item.name)
  const [path, setPath] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [url, setUrl] = useState('')
  const [working, setWorking] = useState(false)
  const [error, setError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    api
      .getOther(item.id)
      .then(setDetail)
      .catch(() => setDetail(null))
  }, [open, item.id, item.status, item.fileCount])

  const state = item.active ? describeState('downloading') : item.status === 'downloaded' ? describeState('downloaded') : null

  async function run(fn: () => Promise<unknown>, done: string) {
    setError('')
    setWorking(true)
    try {
      await fn()
      toast.success(done)
      setPanel('')
      onChanged()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setWorking(false)
    }
  }

  async function remove() {
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: item.name }),
      body: <p>{t('Mediarium cancels anything still downloading for it. You can add it back any time.')}</p>,
      confirmLabel: t('Remove'),
      danger: true,
      option: item.path ? { label: t('Also delete its files'), hint: t("The item's folder and everything in it."), defaultChecked: false } : undefined,
    })
    if (!answer) return
    // No panel is open here, so a failure is told in a message (the error line only shows under a panel).
    try {
      await api.deleteOther(item.id, answer.checked)
      toast.success(t('{title} removed.', { title: item.name }))
      onChanged()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  function submitDownload(e: FormEvent) {
    e.preventDefault()
    if (!file && !url.trim()) return setError(t('Choose an NZB or .torrent file, or paste an address or magnet link.'))
    const form = new FormData()
    form.set('otherId', String(item.id))
    if (file) form.set('file', file)
    else form.set('url', url.trim())
    void run(async () => {
      await api.manualDownload(form)
      setFile(null)
      setUrl('')
    }, t('Download added for {title}.', { title: item.name }))
  }

  return (
    <li className="other-item">
      <div className="other-head">
        <button className="other-name" onClick={() => setOpen((o) => !o)} aria-expanded={open} title={item.name}>
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size={14} />
          <strong>{item.name}</strong>
        </button>
        <span className="other-meta">
          {item.status !== 'downloaded' ? '' : item.fileCount > 1 ? t('{count} files, {size}', { count: String(item.fileCount), size: formatBytes(item.sizeBytes) }) : formatBytes(item.sizeBytes)}
        </span>
        <span className={`other-state st-${state ? state.key : 'none'}`} title={state?.hint}>
          {state ? (
            <>
              <Icon name={state.icon} size={13} /> {state.label}
            </>
          ) : (
            t('No files yet')
          )}
        </span>
        {admin && (
          <span className="other-actions">
            <button className="btn-with-icon" onClick={() => setPanel(panel === 'download' ? '' : 'download')} aria-expanded={panel === 'download'} disabled={item.active}>
              <Icon name="download" size={14} /> {t('Add download')}
            </button>
            <button className="btn-with-icon" onClick={() => setPanel(panel === 'import' ? '' : 'import')} aria-expanded={panel === 'import'} disabled={item.active}>
              <Icon name="folder" size={14} /> {t('Import files')}
            </button>
            <button className="btn-with-icon" onClick={() => setPanel(panel === 'rename' ? '' : 'rename')} aria-expanded={panel === 'rename'}>
              <Icon name="edit" size={14} /> {t('Rename')}
            </button>
            <button className="btn-with-icon danger" onClick={() => void remove()}>
              <Icon name="trash" size={14} /> {t('Remove')}
            </button>
          </span>
        )}
      </div>

      {panel === 'download' && (
        <form className="other-panel" onSubmit={submitDownload}>
          <DownloadFields file={file} setFile={setFile} url={url} setUrl={setUrl} fileInput={fileInput} />
          <div className="form-actions">
            <button className="primary" type="submit" disabled={working}>
              {t('Download')}
            </button>
          </div>
        </form>
      )}
      {panel === 'import' && (
        <form
          className="other-panel"
          onSubmit={(e) => {
            e.preventDefault()
            if (!path.trim()) return setError(t('Choose the file or folder to import.'))
            void run(() => api.importOther(item.id, path.trim()), t('Files imported into {title}.', { title: item.name }))
          }}
        >
          <label>
            {t('File or folder on this server')}
            <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/downloads/Some.Release" spellCheck={false} />
          </label>
          <p className="field-hint">{t('It has to be inside your downloads or library folders. The files are moved, keeping their names.')}</p>
          <div className="form-actions">
            <button className="primary" type="submit" disabled={working}>
              {t('Import')}
            </button>
          </div>
        </form>
      )}
      {panel === 'rename' && (
        <form
          className="other-panel"
          onSubmit={(e) => {
            e.preventDefault()
            void run(() => api.renameOther(item.id, name), t('Renamed.'))
          }}
        >
          <label>
            {t('Name')}
            <input value={name} onChange={(e) => setName(e.target.value)} maxLength={200} />
          </label>
          <p className="field-hint">{t('Files already saved stay in their folder.')}</p>
          <div className="form-actions">
            <button className="primary" type="submit" disabled={working}>
              {t('Save')}
            </button>
          </div>
        </form>
      )}
      {error && panel !== '' && <p className="error" role="alert">{error}</p>}

      {open && (
        <div className="other-files">
          {item.path && <code className="other-path" title={item.path}>{item.path}</code>}
          {detail?.files && detail.files.length > 0 ? (
            <ul>
              {detail.files.map((f) => (
                <li key={f.name}>
                  <span title={f.name}>{f.name}</span>
                  <small className="mono">{formatBytes(f.sizeBytes)}</small>
                </li>
              ))}
            </ul>
          ) : (
            <p className="field-hint">{t('No files yet')}</p>
          )}
        </div>
      )}
    </li>
  )
}
