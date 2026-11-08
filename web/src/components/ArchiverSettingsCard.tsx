import { useCallback, useEffect, useState } from 'react'
import { api, type ArchiverConfig, type ArchiverSource, type ArchiverState } from '../api'
import { st as serverText, t } from '../i18n'
import { when } from './WatchedSettings'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import TipLabel from './TipLabel'
import { useToast } from './Toast'

const FORMATS: { value: ArchiverConfig['format']; label: () => string }[] = [
  { value: 'video', label: () => t('Video, up to 1080p') },
  { value: 'mp3', label: () => t('Audio only, mp3') },
  { value: 'm4a', label: () => t('Audio only, m4a') },
]

const formatName = (f: string) => FORMATS.find((x) => x.value === f)?.label() ?? f

// Settings > Media types: the Channel archiver. It is shown while the module is
// off too, because the warning has to be ticked and the yt-dlp file tested
// before the module's switch will turn it on. The followed channels and the
// recent videos show once it is on.
export default function ArchiverSettingsCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [state, setState] = useState<ArchiverState | null>(null)
  const [cfg, setCfg] = useState<ArchiverConfig | null>(null)
  const [hosts, setHosts] = useState('')
  const [busy, setBusy] = useState('')
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [format, setFormat] = useState<ArchiverConfig['format']>('video')
  const [backlog, setBacklog] = useState(false)

  const take = useCallback((s: ArchiverState, keepForm = false) => {
    setState(s)
    if (!keepForm) {
      setCfg(s.config)
      setHosts(s.config.extraHosts.join(', '))
      setFormat(s.config.format)
    }
  }, [])
  const load = useCallback(
    (keepForm = false) => {
      api
        .archiver()
        .then((s) => take(s, keepForm))
        .catch(() => setState(null))
    },
    [take],
  )
  useEffect(() => load(), [load])

  // While a check runs the list is refreshed every few seconds.
  const checking = !!state?.sources.some((s) => s.checking)
  useEffect(() => {
    if (!checking) return
    const id = window.setTimeout(() => load(true), 3000)
    return () => window.clearTimeout(id)
  }, [checking, state, load])

  async function run(label: string, fn: () => Promise<void>) {
    setBusy(label)
    try {
      await fn()
    } catch (e) {
      toast.error(e instanceof Error ? serverText(e.message) : String(e))
    } finally {
      setBusy('')
    }
  }

  if (!state || !cfg) return null
  const draft = (): ArchiverConfig => ({ ...cfg, ytdlpPath: cfg.ytdlpPath.trim(), folder: cfg.folder.trim(), extraHosts: hosts.split(/[\s,]+/).filter(Boolean) })
  const changed = JSON.stringify(draft()) !== JSON.stringify(state.config)
  const num = (key: 'maxPerRun' | 'pauseSeconds' | 'intervalHours' | 'timeoutMinutes', min: number, max: number) => (
    <input
      type="number"
      min={min}
      max={max}
      value={cfg[key]}
      disabled={!!busy}
      onChange={(e) => setCfg({ ...cfg, [key]: Math.max(min, Math.min(max, Number(e.target.value) || min)) })}
    />
  )

  return (
    <section className="card archiver-card" style={{ ['--mc' as string]: 'var(--c-settings)' }}>
      <h2>{t('Channel archiver')}</h2>
      <div className="archiver-warning" role="note">
        <Icon name="warning" size={18} />
        <div>
          <p>{t("Downloading from YouTube may break YouTube's terms of service.")}</p>
          <p>{t('You are responsible for what you download')}</p>
          <p>{t('and for having the right to keep it.')}</p>
          <p>{t('Mediarium does not include yt-dlp.')}</p>
        </div>
      </div>
      <label className="check-row archiver-tick" style={{ color: 'var(--text)' }}>
        <input type="checkbox" checked={cfg.acknowledged} disabled={!!busy} onChange={(e) => setCfg({ ...cfg, acknowledged: e.target.checked })} />
        <span>{t('I have read this')}</span>
      </label>

      <div className="grid-form archiver-fields">
        <label className="archiver-wide">
          <TipLabel text={t('The full path of the yt-dlp file you supply, like /usr/local/bin/yt-dlp. In Docker, the path inside the container.')}>{t('The yt-dlp file')}</TipLabel>
          <span className="archiver-path">
            <input value={cfg.ytdlpPath} onChange={(e) => setCfg({ ...cfg, ytdlpPath: e.target.value })} placeholder="/usr/local/bin/yt-dlp" spellCheck={false} autoComplete="off" disabled={!!busy} />
            <button
              type="button"
              disabled={!!busy || !cfg.ytdlpPath.trim()}
              onClick={() =>
                void run('test', async () => {
                  const r = await api.testArchiver(cfg.ytdlpPath.trim())
                  toast.success(t('yt-dlp {version} works.', { version: r.version }))
                })
              }
            >
              {busy === 'test' ? t('Checking…') : t('Test')}
            </button>
          </span>
        </label>
        <label>
          <TipLabel text={t('Where the videos go. Mediarium makes it when it is missing.')}>{t('Download folder')}</TipLabel>
          <input value={cfg.folder} onChange={(e) => setCfg({ ...cfg, folder: e.target.value })} placeholder="/archive" spellCheck={false} autoComplete="off" disabled={!!busy} />
        </label>
        <label>
          <TipLabel text={t('What a newly followed channel starts with. Each channel can have its own.')}>{t('Format for new channels')}</TipLabel>
          <select value={cfg.format} disabled={!!busy} onChange={(e) => setCfg({ ...cfg, format: e.target.value as ArchiverConfig['format'] })}>
            {FORMATS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label()}
              </option>
            ))}
          </select>
        </label>
        <label>
          <TipLabel text={t('The most videos downloaded for one channel in one check.')}>{t('Videos per check')}</TipLabel>
          {num('maxPerRun', 1, 50)}
        </label>
        <label>
          <TipLabel text={t('How long to wait between two downloads, so YouTube is not hit all at once.')}>{t('Pause between downloads (seconds)')}</TipLabel>
          {num('pauseSeconds', 0, 3600)}
        </label>
        <label>
          <TipLabel text={t('How often each channel is looked at for new videos.')}>{t('Check every (hours)')}</TipLabel>
          {num('intervalHours', 1, 168)}
        </label>
        <label>
          <TipLabel text={t('A download that takes longer than this is stopped.')}>{t('Longest for one video (minutes)')}</TipLabel>
          {num('timeoutMinutes', 5, 720)}
        </label>
        <label className="archiver-wide">
          <TipLabel text={t('YouTube is allowed already. Any other website has to be added here by name, like vimeo.com, before a channel from it can be followed.')}>{t('Other websites')}</TipLabel>
          <input value={hosts} onChange={(e) => setHosts(e.target.value)} placeholder="vimeo.com" spellCheck={false} autoComplete="off" disabled={!!busy} />
        </label>
      </div>
      <div className="row-actions archiver-actions">
        <button
          className="primary"
          disabled={!!busy || !changed}
          onClick={() =>
            void run('save', async () => {
              take(await api.putArchiver(draft()))
              toast.success(t('Archiver settings saved.'))
            })
          }
        >
          {busy === 'save' ? t('Saving…') : t('Save')}
        </button>
        {!state.enabled && <span className="hint">{t('Save, then switch the Channel archiver on above.')}</span>}
      </div>

      {state.enabled && (
        <>
          <h3>{t('Followed channels and playlists')}</h3>
          <div className="grid-form archiver-fields archiver-follow">
            <label className="archiver-wide">
              <TipLabel text={t('The address of a channel (https://www.youtube.com/@name) or a playlist.')}>{t('Address')}</TipLabel>
              <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://www.youtube.com/@name" spellCheck={false} autoComplete="off" disabled={!!busy} />
            </label>
            <label>
              <TipLabel text={t('The folder the videos go into. Left empty, the channel’s own name is used.')}>{t('Folder name (optional)')}</TipLabel>
              <input value={name} onChange={(e) => setName(e.target.value)} disabled={!!busy} />
            </label>
            <label>
              <TipLabel text={t('The format for this channel.')}>{t('Format')}</TipLabel>
              <select value={format} disabled={!!busy} onChange={(e) => setFormat(e.target.value as ArchiverConfig['format'])}>
                {FORMATS.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label()}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label className="check-row archiver-tick" style={{ color: 'var(--text)' }}>
            <input type="checkbox" checked={backlog} disabled={!!busy} onChange={(e) => setBacklog(e.target.checked)} />
            <span>{t('Also download the videos already there')}</span>
          </label>
          <div className="row-actions archiver-actions">
            <button
              className="primary btn-with-icon"
              disabled={!!busy || !url.trim()}
              onClick={() =>
                void run('follow', async () => {
                  take(await api.addArchiverSource({ url: url.trim(), name: name.trim(), format, backlog }), true)
                  setUrl('')
                  setName('')
                  setBacklog(false)
                })
              }
            >
              <Icon name="plus" size={15} /> {t('Follow')}
            </button>
          </div>

          {state.sources.length === 0 ? (
            <p className="hint">{t('No channels followed yet.')}</p>
          ) : (
            <div className="archiver-table-wrap">
              <table className="archiver-table">
                <thead>
                  <tr>
                    <th>{t('Channel')}</th>
                    <th>{t('Format')}</th>
                    <th>{t('Last checked')}</th>
                    <th>{t('Videos')}</th>
                    <th>{t('Status')}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {state.sources.map((s) => (
                    <SourceRow
                      key={s.id}
                      s={s}
                      disabled={!!busy}
                      onCheck={() => void run('check', async () => take(await api.checkArchiverSource(s.id), true))}
                      onRemove={() =>
                        void run('remove', async () => {
                          if (!(await confirm({ title: t('Stop following this channel?'), body: t('The videos already downloaded stay in your folder.'), confirmLabel: t('Stop following'), danger: true }))) return
                          take(await api.removeArchiverSource(s.id), true)
                        })
                      }
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {state.recent.length > 0 && (
            <div className="archiver-recent">
              <h3>{t('Recent videos')}</h3>
              <ul>
                {state.recent.map((v) => (
                  <li key={`${v.source}-${v.id}`} className={v.state === 'failed' ? 'is-failed' : ''}>
                    <Icon name={v.state === 'failed' ? 'warning' : 'check'} size={13} />
                    <strong>{v.title || v.id}</strong>
                    <span className="hint">
                      {v.source} · {v.state === 'failed' ? `${t('Failed')}: ${serverText(v.error)}` : when(v.at)}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </section>
  )
}

function SourceRow({ s, disabled, onCheck, onRemove }: { s: ArchiverSource; disabled: boolean; onCheck: () => void; onRemove: () => void }) {
  const status = s.checking ? t('Checking…') : s.status === 'error' ? serverText(s.message) : s.lastChecked ? t('Checked') : t('Not checked yet')
  return (
    <tr>
      <td className="archiver-name" title={s.url}>
        <strong>{s.name || s.url}</strong>
      </td>
      <td>{formatName(s.format)}</td>
      <td>{s.lastChecked ? when(s.lastChecked) : t('never')}</td>
      <td>{t('{done} kept · {queued} waiting · {failed} failed', { done: s.done, queued: s.queued, failed: s.failed })}</td>
      <td className={s.status === 'error' && !s.checking ? 'error-text' : ''}>{status}</td>
      <td className="archiver-row-actions">
        <button className="btn-sm" disabled={disabled || s.checking} onClick={onCheck}>
          {t('Check now')}
        </button>
        <button className="btn-sm" disabled={disabled} onClick={onRemove}>
          {t('Remove')}
        </button>
      </td>
    </tr>
  )
}
