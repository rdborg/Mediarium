import { useCallback, useEffect, useRef, useState } from 'react'
import { api, TORRENT_ROUTE_EVENT, type DownloadClientInput, type DownloadClientKind, type DownloadClientSettings } from '../api'
import { t } from '../i18n'
import Icon from './Icon'
import { useToast } from './Toast'
import TipLabel from './TipLabel'
import { reloadForm } from './downloadClientForm'

const LABEL: Record<Exclude<DownloadClientKind, ''>, string> = {
  qbittorrent: 'qBittorrent',
  transmission: 'Transmission',
  rdtclient: 'RDT-Client',
  decypharr: 'Decypharr',
}

const blank: DownloadClientInput = {
  kind: '',
  url: '',
  username: '',
  password: '',
  category: '',
  categoryMovies: '',
  categoryTv: '',
  categoryMusic: '',
  categoryBooks: '',
  remotePath: '',
  localPath: '',
  removeAfterImport: false,
  removeOnStop: true,
  linkRoot: '',
}

function toInput(s: DownloadClientSettings): DownloadClientInput {
  return {
    kind: s.kind,
    url: s.url,
    username: s.username,
    password: '',
    category: s.category,
    categoryMovies: s.categoryMovies,
    categoryTv: s.categoryTv,
    categoryMusic: s.categoryMusic,
    categoryBooks: s.categoryBooks,
    remotePath: s.remotePath,
    localPath: s.localPath,
    removeAfterImport: s.removeAfterImport,
    removeOnStop: s.removeOnStop,
    linkRoot: s.linkRoot,
  }
}

// Settings > Downloading: send torrent releases to a download client you
// already run (qBittorrent, or one that works like it: RDT-Client,
// Decypharr). The client downloads and seeds; Mediarium imports the finished
// files from its folder without moving them.
export default function DownloadClientCard() {
  const toast = useToast()
  const [form, setForm] = useState<DownloadClientInput>(blank)
  const [saved, setSaved] = useState<DownloadClientSettings | null>(null)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; version?: string; error?: string } | null>(null)

  const [loadFailed, setLoadFailed] = useState(false)
  // The saved copy the form was last in step with, so a reload keeps what the
  // person typed and did not save.
  const savedRef = useRef<DownloadClientSettings | null>(null)

  const load = useCallback(() => {
    api
      .downloadClient()
      .then((d) => {
        const was = savedRef.current ? toInput(savedRef.current) : blank
        savedRef.current = d
        setSaved(d)
        setForm((f) => reloadForm(f, was, toInput(d)))
        setLoadFailed(false)
      })
      .catch(() => setLoadFailed(true))
  }, [])
  useEffect(load, [load])
  useEffect(() => {
    window.addEventListener(TORRENT_ROUTE_EVENT, load)
    return () => window.removeEventListener(TORRENT_ROUTE_EVENT, load)
  }, [load])

  function set<K extends keyof DownloadClientInput>(key: K, value: DownloadClientInput[K]) {
    setForm((f) => ({ ...f, [key]: value }))
    setResult(null)
  }

  const passwordKept = !!saved?.hasPassword && form.url === saved.url && form.password === ''
  const savedInput = saved ? toInput(saved) : blank
  const changed = (Object.keys(form) as (keyof DownloadClientInput)[]).some((k) => form[k] !== savedInput[k])
  const name = form.kind ? LABEL[form.kind] : ''

  async function test() {
    setBusy(true)
    setResult(null)
    try {
      setResult(await api.testDownloadClient(form))
    } catch (e) {
      setResult({ ok: false, error: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(false)
    }
  }

  async function save() {
    setBusy(true)
    try {
      const d = await api.saveDownloadClient(form)
      savedRef.current = d
      setSaved(d)
      setForm(toInput(d))
      window.dispatchEvent(new Event(TORRENT_ROUTE_EVENT))
      toast.success(d.kind ? t('Saved. Torrent releases now go to {name}.', { name: LABEL[d.kind as 'qbittorrent'] }) : t('Saved. Torrents are downloaded here again.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <fieldset className="group torrent">
      <legend>
        <Icon name="server" size={14} /> <TipLabel help="downloads#download-clients-qbittorrent-transmission-rdt-client-decypharr">{t('Use your own torrent client')}</TipLabel>
      </legend>
      <p style={{ marginTop: 0 }}>
        {t('Hand torrent releases to a download client you already run: qBittorrent, Transmission, or a program that works like qBittorrent, such as RDT-Client or Decypharr. The client downloads and seeds, and Mediarium imports the finished files from its folder without moving them.')}
      </p>
      <p className="field-help">{t('The download client and a debrid service can both be set up. Torrents go to the client first; an indexer can send its torrents to the debrid service or the built-in client instead (Settings > Indexers & Search, Where torrents go).')}</p>
      <div className="form-cols">
        <label>
          {t('Client')}
          <select value={form.kind} onChange={(e) => set('kind', e.target.value as DownloadClientKind)}>
            <option value="">{t('None (download torrents here)')}</option>
            <option value="qbittorrent">qBittorrent</option>
            <option value="transmission">Transmission</option>
            <option value="rdtclient">RDT-Client</option>
            <option value="decypharr">Decypharr</option>
          </select>
        </label>
        {form.kind && (
          <>
            <label>
              {t('Address')}
              <input value={form.url} onChange={(e) => set('url', e.target.value)} placeholder={form.kind === 'transmission' ? 'http://192.168.1.10:9091' : 'http://192.168.1.10:8080'} autoComplete="off" spellCheck={false} />
              <small className="field-help">{form.kind === 'transmission' ? t('Where you open its web page, for example http://192.168.1.10:9091. Mediarium adds /transmission/rpc itself.') : t('Where you open its web page.')}</small>
            </label>
            <label>
              {t('User name')}
              <input value={form.username} onChange={(e) => set('username', e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('Password')}
              <input
                type="password"
                value={form.password}
                onChange={(e) => set('password', e.target.value)}
                placeholder={passwordKept ? t('Saved. Type a new one to replace it.') : ''}
                autoComplete="new-password"
              />
            </label>
            <label>
              {t('Category')}
              <input value={form.category} onChange={(e) => set('category', e.target.value)} placeholder="mediarium" autoComplete="off" spellCheck={false} />
              <small className="field-help">{form.kind === 'transmission' ? t('Torrents get this label, so they are easy to find in Transmission (it needs version 3 or newer). If you clear it and fill in a label below, a kind with none of its own gets no label. With everything empty, "mediarium" is used.') : t('Torrents are added in this category, so they are easy to find in the client. If you clear it and fill in a category below, a kind with none of its own gets no category. With everything empty, "mediarium" is used.')}</small>
            </label>
            <label>
              {t('Category for movies')}
              <input value={form.categoryMovies} onChange={(e) => set('categoryMovies', e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('Category for TV shows')}
              <input value={form.categoryTv} onChange={(e) => set('categoryTv', e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('Category for music')}
              <input value={form.categoryMusic} onChange={(e) => set('categoryMusic', e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('Category for books')}
              <input value={form.categoryBooks} onChange={(e) => set('categoryBooks', e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
            <small className="field-help" style={{ gridColumn: '1 / -1' }}>{t('The four categories above are optional. Fill one in to keep that kind of download apart in the client (a label in Transmission). An empty one uses the main category.')}</small>
          </>
        )}
      </div>
      {form.kind && (
        <>
          <h3 style={{ margin: '18px 0 4px', fontSize: '1rem' }}><TipLabel help="downloads#path-mapping">{t('Path mapping')}</TipLabel></h3>
          <p className="field-help">
            {t('Only needed when the client runs in another container or on another computer and sees its download folder under another path. Example: the client saves to /downloads, and Mediarium sees the same folder as /data/torrents.')}
          </p>
          <div className="form-cols">
            <label>
              {t("The client's folder")}
              <input value={form.remotePath} onChange={(e) => set('remotePath', e.target.value)} placeholder="/downloads" autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('The same folder in Mediarium')}
              <input value={form.localPath} onChange={(e) => set('localPath', e.target.value)} placeholder="/data/torrents" autoComplete="off" spellCheck={false} />
            </label>
            <label>
              {t('Symlinks may point into')}
              <input value={form.linkRoot} onChange={(e) => set('linkRoot', e.target.value)} placeholder="/mnt/remote" autoComplete="off" spellCheck={false} />
              <small className="field-help">
                {t('For clients that hand over symlinks to a mounted cloud drive, like Decypharr. Enter that mount as Mediarium sees it, and the symlinks are added to your library as symlinks. Leave it empty when your client saves real files.')}
              </small>
            </label>
          </div>
          <label className="check-hint">
            <input type="checkbox" checked={form.removeOnStop} onChange={(e) => set('removeOnStop', e.target.checked)} />
            <span>
              {t('When I stop or remove a download, remove it from the client too')}
              <small>{t("Only for a torrent that Mediarium sent and that hasn't finished: it is removed with what it downloaded. A finished torrent, and one that was in the client before, stays. Blocklist & search again also removes a finished one.")}</small>
            </span>
          </label>
          <label className="check-hint">
            <input type="checkbox" checked={form.removeAfterImport} onChange={(e) => set('removeAfterImport', e.target.checked)} />
            <span>
              {t('Remove from the client after import')}
              <small>{t('Off: the torrent stays in the client and keeps seeding. On: it is removed once it is in your library, with its files unless they are symlinks.')}</small>
            </span>
          </label>
        </>
      )}
      <div className="form-actions" style={{ justifyContent: 'flex-start' }}>
        {form.kind && (
          <button onClick={() => void test()} disabled={busy || !form.url}>
            {busy ? t('Testing…') : t('Test')}
          </button>
        )}
        <button className="primary" onClick={() => void save()} disabled={busy || !changed || (form.kind !== '' && !form.url)}>
          {t('Save')}
        </button>
      </div>
      {loadFailed && (
        <div className="indexer-result bad" role="alert">
          <Icon name="warning" size={14} /> {t("Couldn't load the download client settings. Try reloading the page.")}
        </div>
      )}
      {result && (
        <div className={`indexer-result ${result.ok ? 'ok' : 'bad'}`}>
          <Icon name={result.ok ? 'check' : 'warning'} size={14} /> {result.ok ? t('Connected to {name} {version}.', { name, version: result.version ?? '' }) : result.error}
        </div>
      )}
    </fieldset>
  )
}
