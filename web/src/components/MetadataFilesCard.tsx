import { useCallback, useEffect, useState } from 'react'
import { api, type MetadataFileCounts, type MetadataFilesSettings, type MetadataFilesStatus } from '../api'
import { DOCS_URL } from '../docs'
import { plural, t, st } from '../i18n'
import { useLive } from '../useLive'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'

type Kind = 'movies' | 'tv'

const total = (c: MetadataFileCounts) => c.nfo + c.posters + c.fanart + c.logos + c.seasonPosters + c.episodeNfos

// Settings > Library and files > Folders: NFO and artwork files that
// Kodi, Jellyfin and Emby read (and the artwork Plex reads) next to the videos.
// Everything is off until switched on; a file that is already there is never
// overwritten unless "replace" is chosen.
export default function MetadataFilesCard() {
  const toast = useToast()
  const [cfg, setCfg] = useState<MetadataFilesSettings | null>(null)
  const [status, setStatus] = useState<MetadataFilesStatus | null>(null)
  const [preview, setPreview] = useState<MetadataFilesStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .getMetadataFiles()
      .then(setCfg)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.metadataFilesStatus().then(setStatus).catch(() => undefined)
  }, [])
  useEffect(load, [load])
  const running = !!status?.running
  useLive(() => api.metadataFilesStatus().then(setStatus).catch(() => undefined), 1000, running)

  async function save(next: MetadataFilesSettings) {
    setCfg(next)
    setPreview(null)
    try {
      setCfg(await api.putMetadataFiles({ movies: next.movies, tv: next.tv, replace: next.replace }))
      toast.success(t('Saved.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      load()
    }
  }

  function toggle(kind: Kind, field: 'nfo' | 'artwork', on: boolean) {
    if (!cfg) return
    void save({ ...cfg, [kind]: { ...cfg[kind], [field]: on } })
  }

  async function count() {
    setBusy(true)
    setError('')
    try {
      setPreview(await api.runMetadataFiles({ dryRun: true }))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function start() {
    setBusy(true)
    setError('')
    try {
      setStatus(await api.runMetadataFiles({}))
      setPreview(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function stop() {
    try {
      setStatus(await api.cancelMetadataFiles())
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (!cfg) return error ? <p className="error-text">{error}</p> : null
  const anyOn = cfg.movies.nfo || cfg.movies.artwork || cfg.tv.nfo || cfg.tv.artwork
  const finished = status && !status.running && status.finishedAt && !status.dryRun

  return (
    <fieldset className="group span-all">
      <legend>
        <Icon name="image" size={14} /> {t('NFO and artwork files')}
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Kodi, Jellyfin and Emby read small files next to your videos: an NFO file with the title, plot, cast and the TMDB, IMDb and TVDB ids, and the poster and background pictures. Plex does not read NFO files. Both are off until you switch them on. A file that is already there is never overwritten.')}{' '}
        <a href={`${DOCS_URL}/media-servers.md#nfo-and-artwork-files`} target="_blank" rel="noreferrer">
          {t('More about these files')}
        </a>
      </p>
      {!cfg.hasTmdbKey && <p className="error-text">{t('These files are made from TMDB data, so add your TMDB key first (Settings > Library and files > Movie and show info).')}</p>}
      <div className="half-cols">
        <div style={{ display: 'grid', gap: 10, alignContent: 'start' }}>
          <strong>{t('Movies')}</strong>
          <Switch checked={cfg.movies.nfo} onChange={(v) => toggle('movies', 'nfo', v)} showState label={t('NFO file')} description={t('movie.nfo in the movie folder.')} />
          <Switch checked={cfg.movies.artwork} onChange={(v) => toggle('movies', 'artwork', v)} showState label={t('Artwork')} description={t('poster.jpg, fanart.jpg and clearlogo.png when there is one.')} />
        </div>
        <div style={{ display: 'grid', gap: 10, alignContent: 'start' }}>
          <strong>{t('TV shows')}</strong>
          <Switch checked={cfg.tv.nfo} onChange={(v) => toggle('tv', 'nfo', v)} showState label={t('NFO files')} description={t('tvshow.nfo in the show folder, and one for each episode.')} />
          <Switch checked={cfg.tv.artwork} onChange={(v) => toggle('tv', 'artwork', v)} showState label={t('Artwork')} description={t('poster.jpg, fanart.jpg, clearlogo.png when there is one, and a poster for each season.')} />
        </div>
      </div>
      <label style={{ display: 'grid', gap: 4, maxWidth: 460, marginTop: 14 }}>
        <span>{t('If a file is already there')}</span>
        <select value={cfg.replace ? 'replace' : 'keep'} onChange={(e) => void save({ ...cfg, replace: e.target.value === 'replace' })}>
          <option value="keep">{t('Keep it')}</option>
          <option value="replace">{t('Replace the ones Mediarium wrote')}</option>
        </select>
        <small className="field-hint">{t("Replacing only touches files with the exact names Mediarium uses. Pictures and NFO files you added under other names, like folder.jpg, are never touched.")}</small>
      </label>

      <div style={{ marginTop: 18, display: 'grid', gap: 10 }}>
        <strong>{t('Write them for what is already in my library')}</strong>
        <p className="field-hint" style={{ margin: 0 }}>
          {t('New imports get the files by themselves. This does the same for the movies and shows you already have, using the switches above.')}
        </p>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button onClick={() => void count()} disabled={busy || running || !anyOn}>
            {t('Count what would be written')}
          </button>
          <button className="primary" onClick={() => void start()} disabled={busy || running || !anyOn || !cfg.hasTmdbKey}>
            {t('Write the files')}
          </button>
          {running && (
            <button onClick={() => void stop()}>{t('Stop')}</button>
          )}
        </div>
        {preview && !running && (
          <p role="status" style={{ margin: 0 }}>
            <strong>{st(preview.message)}</strong>
            {total(preview.needed) > 0 && <span className="field-hint"> {countsText(preview.needed)}</span>}
          </p>
        )}
        {running && status && (
          <div role="status">
            <div className="bar active" style={{ maxWidth: 460 }}>
              <span style={{ width: `${status.titles ? Math.max(3, (status.done / status.titles) * 100) : 3}%` }} />
            </div>
            <small className="field-hint">
              {t('{done} of {total} titles', { done: status.done, total: status.titles })}
              {status.current ? ` · ${status.current}` : ''} · {plural(total(status.written), '{n} file written', '{n} files written')}
            </small>
          </div>
        )}
        {finished && status && !preview && (
          <div role="status">
            <p style={{ margin: 0 }}>{st(status.message)}</p>
            {status.failedCount > 0 && (
              <ul className="field-hint" style={{ margin: '6px 0 0', paddingLeft: 18 }}>
                {status.failed.slice(0, 5).map((f, i) => (
                  <li key={i}>{f}</li>
                ))}
                {status.failedCount > 5 && <li>{plural(status.failedCount - 5, '…and {n} more', '…and {n} more')}</li>}
              </ul>
            )}
          </div>
        )}
        {error && <p className="error-text">{error}</p>}
      </div>
    </fieldset>
  )
}

function countsText(c: MetadataFileCounts): string {
  const parts: string[] = []
  if (c.nfo) parts.push(plural(c.nfo, '{n} NFO file', '{n} NFO files'))
  if (c.episodeNfos) parts.push(plural(c.episodeNfos, '{n} episode NFO file', '{n} episode NFO files'))
  if (c.posters) parts.push(plural(c.posters, '{n} poster', '{n} posters'))
  if (c.fanart) parts.push(plural(c.fanart, '{n} background', '{n} backgrounds'))
  if (c.logos) parts.push(plural(c.logos, 'up to {n} logo', 'up to {n} logos'))
  if (c.seasonPosters) parts.push(plural(c.seasonPosters, '{n} season poster', '{n} season posters'))
  return `(${parts.join(', ')})`
}
