import DebridCard from '../../components/DebridCard'
import DownloadChecksCard, { UsenetRetentionCard } from '../../components/DownloadChecksCard'
import DownloadClientCard from '../../components/DownloadClientCard'
import SoulseekCard from '../../components/SoulseekCard'
import { useModules } from '../../ModulesContext'
import UsenetClientCard from '../../components/UsenetClientCard'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type DownloadsStatus, type Settings, type UsenetServer } from '../../api'
import FolderStatus from '../../components/FolderStatus'
import Icon from '../../components/Icon'
import Switch from '../../components/Switch'
import UsenetServerForm, { blankServer, type UsenetServerDraft } from '../../components/UsenetServerForm'
import TestButton from '../../components/TestButton'
import { useToast } from '../../components/Toast'
import { useAutosaveSetting } from '../../useAutosave'
import TorrentSection from './TorrentSection'
import TorrentOptionsPanel from '../../components/TorrentOptionsPanel'
import { useLive } from '../../useLive'
import { useConfirm } from '../../components/ConfirmProvider'
import { plural, t, tx, st } from '../../i18n'
import InfoTip from '../../components/InfoTip'
import TipLabel from '../../components/TipLabel'
import SpeedSchedule from '../../components/SpeedSchedule'
import { firstError, numberRange } from '../../validate'

// Downloading: Mediarium downloads for itself. Usenet needs only your
// provider's news-server account; torrents need nothing at all. There is no
// separate download client program to install or connect (unlike Sonarr and
// Radarr, which hand grabs to SABnzbd, qBittorrent and the like), though a
// torrent client you already run can be connected (DownloadClientCard).
// The settings are on three pages: the rules both share, Usenet and torrents.
function useDownloadsState() {
  const toast = useToast()
  const [status, setStatus] = useState<DownloadsStatus | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const loadStatus = useCallback(() => {
    api.downloadsStatus().then(setStatus).catch(() => undefined)
  }, [])
  const loadSettings = useCallback(() => {
    api.getSettings().then(setSettings).catch(() => undefined)
  }, [])
  useEffect(loadStatus, [loadStatus])
  useLive(loadStatus, 10000)
  useEffect(loadSettings, [loadSettings])

  // One click from a blurred section's notice: which sources to use.
  async function chooseSources(value: 'both' | 'usenet' | 'torrent') {
    try {
      const saved = await api.putSettings({ defaultSources: value, torrentEnabled: value !== 'usenet' })
      setSettings(saved)
      toast.success(value === 'both' ? t('Now downloading from Usenet and torrents.') : value === 'usenet' ? t('Now downloading from Usenet only.') : t('Now downloading from torrents only.'))
      loadStatus()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function setTorrentEnabled(v: boolean) {
    try {
      const saved = await api.putSettings({ torrentEnabled: v })
      setSettings(saved)
      toast.success(v ? t('Torrent client turned on.') : t('Torrent client turned off.'))
      loadStatus()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  // The Usenet and torrent switches: each turns one downloader on or off.
  // At least one stays on, because with both off nothing could download.
  async function setSource(kind: 'usenet' | 'torrent', on: boolean) {
    const torrentNow = settings?.defaultSources !== 'usenet' && settings?.torrentEnabled !== false
    const usenetNow = settings?.defaultSources !== 'torrent'
    const usenet = kind === 'usenet' ? on : usenetNow
    const torrent = kind === 'torrent' ? on : torrentNow
    if (!usenet && !torrent) {
      toast.error(t('Keep at least one of Usenet and torrents on, or nothing could download.'))
      return
    }
    await chooseSources(usenet && torrent ? 'both' : usenet ? 'usenet' : 'torrent')
  }
  return { status, settings, loadStatus, loadSettings, chooseSources, setTorrentEnabled, setSource }
}

// Settings > Downloading > Download rules: the rules that apply to Usenet and
// torrents alike.
export default function DownloadRulesSettings() {
  const { settings, loadSettings, setTorrentEnabled } = useDownloadsState()
  const musicOn = useModules().on('music') // Soulseek is for music only
  return (
    <div className="settings-stack">
      <div className="half-cols span-all">
      <fieldset className="group folders">
        <legend>
          <Icon name="download" size={14} /> <TipLabel help="downloads#the-downloading-pages">{t('Where to download from')}</TipLabel>
        </legend>
        <DefaultSourcesCard
          key={settings?.defaultSources ?? 'loading'}
          onChanged={async (value) => {
            // Choosing Usenet only also switches the torrent client off, and choosing torrents back on again.
            const wantTorrent = value !== 'usenet'
            if (wantTorrent !== (settings?.torrentEnabled !== false)) await setTorrentEnabled(wantTorrent)
            else loadSettings()
          }}
        />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="list" size={14} /> <TipLabel help="downloads#downloads-at-the-same-time-the-download-line">{t('Downloads at the same time')}</TipLabel>
        </legend>
        <DownloadsAtOnceCard />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="download" size={14} /> <TipLabel help="downloads#speed-and-space">{t('Speed, hours and free space')}</TipLabel>
        </legend>
        <SpeedAndSpaceCard />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="folder" size={14} /> <TipLabel help="downloads#temporary-download-folder">{t('Temporary download folder')}</TipLabel>
        </legend>
        <TempFolderCard key={settings ? 'loaded' : 'loading'} saved={settings?.downloadsTempPath ?? ''} ready={settings !== null} onSaved={loadSettings} />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="moon" size={14} /> <TipLabel help="downloads#speed-schedule">{t('Speed schedule')}</TipLabel>
        </legend>
        <SpeedSchedule />
      </fieldset>
      <fieldset className="group folders">
        <legend>
          <Icon name="shield" size={14} /> <TipLabel help="downloads#checks-on-releases">{t('Checks on releases')}</TipLabel>
        </legend>
        <DownloadChecksCard />
      </fieldset>
      {musicOn && <SoulseekCard />}
      </div>
    </div>
  )
}

// Settings > Downloading > Usenet: the provider login and the built-in downloader.
export function UsenetSettings() {
  const { status, settings, loadStatus, setSource } = useDownloadsState()
  const torrentOnly = settings?.defaultSources === 'torrent'
  return (
    <div className="settings-stack">
      <fieldset className={`group usenet span-all${torrentOnly ? ' section-off' : ''}`}>
        <legend>
          <Icon name="server" size={14} /> Usenet (NZB)
        </legend>
        <Switch
          checked={!torrentOnly}
          onChange={(v) => void setSource('usenet', v)}
          label={t('Download from Usenet')}
          description={t('Turn it off if you only use torrents.')}
          showState
        />
        <div className="half-cols section-body" style={{ marginTop: 14 }}>
          <div className="col-stack">
          <section className="card">
            <h2>
              <TipLabel help="downloads#usenet-connections">{t('Downloader')}</TipLabel>{' '}
              {status?.usenet.external ? (
                <span className="badge downloaded">{status.usenet.externalLabel}</span>
              ) : (
                <span className="badge downloaded">{t('built in')}</span>
              )}{' '}
              {status && <span className={`badge ${status.usenet.ready ? 'downloaded' : 'missing'}`}>{status.usenet.ready ? t('ready') : t('needs your provider')}</span>}
            </h2>
            <p style={{ color: 'var(--text-dim)' }}>
              {status?.usenet.external
                ? t('Usenet releases go to {name} right now. The built-in downloader is only used again when you switch that off in the last card on this page.', { name: status.usenet.externalLabel ?? '' })
                : status && !status.usenet.ready
                  ? t("Built in, so there's no SABnzbd or NZBGet to set up. It only needs your Usenet provider's login, which you add in the next card.")
                  : t("Built in, so there's no SABnzbd or NZBGet to set up. It only needs your Usenet provider's login.")}
            </p>
          </section>
          <UsenetRetentionCard />
          </div>
          <UsenetServersSection onChange={loadStatus} />
        </div>
      </fieldset>
      <UsenetClientCard onChanged={loadStatus} />
    </div>
  )
}

// Settings > Downloading > Torrents: everything about downloading torrents.
// The VPN and proxy have their own page next to this one.
export function TorrentSettings() {
  const { status, settings, setSource } = useDownloadsState()
  const usenetOnly = settings?.defaultSources === 'usenet'
  const torrentEnabled = settings?.torrentEnabled !== false && !usenetOnly
  const torrentOff = !torrentEnabled
  return (
    <div className="settings-stack">
      <fieldset className={`group torrent span-all${torrentOff ? ' section-off' : ''}`}>
        <legend>
          <Icon name="magnet" size={14} /> {t('Torrents')}
        </legend>
        {/* The on/off switch stays outside the blurred body, so it can always be pressed. */}
        <Switch
          checked={torrentEnabled}
          onChange={(v) => void setSource('torrent', v)}
          label={t('Download from torrents')}
          description={t('Turn it off if you only use Usenet.')}
          showState
        />
        <div className="section-body" style={{ marginTop: 14 }}>
        <fieldset disabled={torrentOff} className="plain-fieldset">
          <div className="half-cols" style={{ marginTop: 14 }}>
            <div className="col-stack">
            <section className="card">
              <h2>
                {t('Client')} <span className="badge downloaded">{t('built in')}</span>{' '}
                {status && (
                  <span className={`badge ${torrentOff ? '' : status.torrent.ready ? 'downloaded' : 'missing'}`}>
                    {torrentOff ? t('off') : status.torrent.ready ? t('ready') : t('waiting for the VPN')}
                  </span>
                )}
              </h2>
              <p style={{ color: 'var(--text-dim)' }}>
                {t("Built in too, so there's nothing to install and no login. Torrents come from the indexers you add under Settings > Indexers & Search.")}
                {status?.torrent.vpnRequired && (
                  <>
                    {' '}
                    {status.torrent.vpnConnected
                      ? t('The VPN kill switch is on, so torrents only run while a VPN is connected (connected now).')
                      : t('The VPN kill switch is on, so torrents only run while a VPN is connected (not connected).')}{' '}
                    <Link to="../vpn">{t('VPN settings')}</Link>
                  </>
                )}
                {status?.torrent.blockedByVpn && !status.torrent.vpnRequired && (
                  <>
                    {' '}
                    {t('Your VPN is on but not connected, so torrents wait for it.')} <Link to="../vpn">{t('VPN settings')}</Link>
                  </>
                )}
              </p>
              {status && status.torrent.vpnState === 'off' && !status.torrent.vpnRequired && !torrentOff && (
                <div className="notice notice-warn">
                  <strong>{t('Use a VPN for torrents.')}</strong> {t('Without one, everyone in the swarm can see your home IP address.')} <Link to="../vpn">{t('Set up a VPN')}</Link>
                </div>
              )}
            </section>
            <TorrentSection status={status?.torrent ?? null} />
            <StalledTorrentCard />
            </div>
            <div className="col-stack">
            <section className="card">
              <h2><TipLabel help="downloads#controls-of-the-built-in-torrent-client">{t('Torrent options')}</TipLabel></h2>
              <TorrentOptionsPanel variant="settings" listenPort={status?.torrent.listenPort ?? 58264} />
            </section>
            </div>
          </div>
        </fieldset>
        </div>
      </fieldset>
      <div className="half-cols span-all">
        <DebridCard />
        <DownloadClientCard />
      </div>
    </div>
  )
}

// Which downloaders new movies and shows may use unless you say otherwise
// when adding them (or later on the item's own page).
function DefaultSourcesCard({ onChanged }: { onChanged: (value: string) => void | Promise<void> }) {
  const sources = useAutosaveSetting<string>(
    (s) => s.defaultSources ?? 'both',
    (v) => ({ defaultSources: v as 'both' | 'usenet' | 'torrent' }),
    (saved) =>
      saved === 'both'
        ? t('Saved: new titles download from Usenet and torrents.')
        : saved === 'usenet'
          ? t('Saved: new titles download from Usenet only.')
          : t('Saved: new titles download from torrents only.'),
    'both',
  )
  return (
    <div>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Used for every movie and show unless you change it on the title. Usenet wins when both are equally good.')}
      </p>
      <div className="seg" role="radiogroup" aria-label={t('Default downloaders')}>
        {[
          ['both', t('Usenet and torrents')],
          ['usenet', t('Usenet only')],
          ['torrent', t('Torrents only')],
        ].map(([value, label]) => (
          <button
            key={value}
            role="radio"
            aria-checked={sources.value === value}
            className={sources.value === value ? 'active' : ''}
            disabled={!sources.loaded || sources.saving}
            onClick={() => void sources.change(value).then(() => onChanged(value))}
          >
            {label}
          </button>
        ))}
      </div>
    </div>
  )
}

// The highest "Downloads at the same time" the server accepts (maxDownloadsAtOnce
// in internal/api/dispatch.go).
const MAX_DOWNLOADS_AT_ONCE = 500

// How many downloads run together. Usenet and torrents share this number: the
// rest wait in line, and the next one starts by itself when one is finished
// (downloaded, repaired, unpacked and moved into your library). A number box,
// not a few buttons, because a download client such as qBittorrent can run a
// hundred torrents side by side. It saves a moment after you stop typing, or
// when you leave the box, and only a whole number in range.
function DownloadsAtOnceCard() {
  const atOnce = useAutosaveSetting<number>(
    (s) => s.downloadsAtOnce ?? 1,
    (v) => ({ downloadsAtOnce: v }),
    (saved) => (saved === 1 ? t('Saved: one download at a time.') : t('Saved: up to {n} downloads at the same time.', { n: saved })),
    1,
  )
  // What is being typed; null shows the saved number.
  const [atOnceDraft, setAtOnceDraft] = useState<string | null>(null)
  const atOnceText = atOnceDraft ?? String(atOnce.value)
  const atOnceTimer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(atOnceTimer.current), [])
  const atOnceError = firstError(
    atOnceText.trim() === '' && t('Enter a number from {min} to {max}.', { min: 1, max: MAX_DOWNLOADS_AT_ONCE }),
    numberRange(atOnceText, t('The number'), { min: 1, max: MAX_DOWNLOADS_AT_ONCE }),
  )
  const saveAtOnce = (text: string) => {
    window.clearTimeout(atOnceTimer.current)
    if (!atOnce.loaded || numberRange(text, t('The number'), { min: 1, max: MAX_DOWNLOADS_AT_ONCE, allowBlank: false })) return
    const n = Number(text.trim())
    // Back to showing the saved number; a refused save puts the old one back.
    setAtOnceDraft(null)
    if (n !== atOnce.value) void atOnce.change(n)
  }
  const editAtOnce = (text: string) => {
    setAtOnceDraft(text)
    window.clearTimeout(atOnceTimer.current)
    atOnceTimer.current = window.setTimeout(() => saveAtOnce(text), 1200)
  }
  const newest = useAutosaveSetting<boolean>(
    (s) => s.newestFirst ?? false,
    (v) => ({ newestFirst: v }),
    (v) => (v ? t('Saved: newer releases start first.') : t('Saved: downloads start in the order they were added.')),
    false,
  )
  const resume = useAutosaveSetting<boolean>(
    (s) => s.resumeInterrupted ?? true,
    (v) => ({ resumeInterrupted: v }),
    (v) => (v ? t('Saved: interrupted downloads carry on after a restart.') : t('Saved: interrupted downloads stay paused after a restart.')),
    true,
  )
  return (
    <div>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        {t('Usenet and torrents share this number. The rest wait in line.')}
      </p>
      <input
        inputMode="numeric"
        aria-label={t('Downloads at the same time')}
        value={atOnceText}
        disabled={!atOnce.loaded || atOnce.saving}
        onChange={(e) => editAtOnce(e.target.value)}
        onBlur={(e) => saveAtOnce(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && saveAtOnce(atOnceText)}
        aria-invalid={atOnceError ? true : undefined}
        style={{ width: 80 }}
      />
      {atOnceError && (
        <small style={{ display: 'block', color: 'var(--danger)' }}>{atOnceError}</small>
      )}
      <label className="inline-field" style={{ marginTop: 12 }}>
        <input type="checkbox" checked={newest.value} disabled={!newest.loaded || newest.saving} onChange={(e) => void newest.change(e.target.checked)} />
        {t('Start newer releases first')}
      </label>
      <p className="field-hint">{t('Among the downloads Mediarium finds by itself, the movie that came out (or the episode that aired) most recently starts first. What you add by hand, or move to the front, still goes first.')}</p>
      <label className="inline-field" style={{ marginTop: 12 }}>
        <input type="checkbox" checked={resume.value} disabled={!resume.loaded || resume.saving} onChange={(e) => void resume.change(e.target.checked)} />
        {t('Resume interrupted downloads after a restart')}
      </label>
      <p className="field-hint">{t('When Mediarium restarts, the downloads that were running carry on by themselves, a few seconds apart. Ones you paused stay paused. Nothing starts in safe mode, while someone is watching (if you pause for that), or before your VPN is up.')}</p>
    </div>
  )
}

// A folder (on a fast drive) for the unfinished part of downloads. Empty keeps
// everything in the downloads folder, as before. Saved with a button because a
// wrong path is refused with a message, not silently changed.
function TempFolderCard({ saved, ready, onSaved }: { saved: string; ready: boolean; onSaved: () => void }) {
  const toast = useToast()
  const [value, setValue] = useState(saved)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function save(next: string) {
    setBusy(true)
    setError('')
    try {
      const result = await api.putSettings({ downloadsTempPath: next.trim() })
      setValue(result.downloadsTempPath ?? '')
      toast.success(next.trim() === '' ? t('Saved: unfinished downloads stay in the downloads folder.') : t('Saved: unfinished downloads are worked on in {folder}.', { folder: next.trim() }))
      onSaved()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  const changed = value.trim() !== saved
  return (
    <div>
      <label className="inline-field">
        <input
          type="text"
          aria-label={t('Temporary download folder')}
          placeholder="/mnt/cache/downloads"
          value={value}
          disabled={!ready || busy}
          onChange={(e) => {
            setValue(e.target.value)
            setError('')
          }}
        />
        <button className="primary" disabled={!ready || busy || !changed} onClick={() => void save(value)}>
          {t('Save')}
        </button>
        {saved !== '' && (
          <button disabled={busy} onClick={() => void save('')}>
            {t('Turn off')}
          </button>
        )}
      </label>
      {error !== '' && <p className="error-text">{error}</p>}
      {value.trim() !== '' && <FolderStatus path={value.trim()} />}
      <p className="field-hint">
        {t('Leave this empty and downloads are worked on in the downloads folder. Set a folder on a fast drive, such as a cache drive, and the parts being downloaded, the repair and the unpacking happen there. When a download is finished, the movie or episode is moved into your library from there.')}
      </p>
      <p className="field-hint">{t('A download that has already started stays where it is when you change this. In Docker, mount the folder into the container yourself first, then enter the path as the container sees it.')}</p>
    </div>
  )
}

const SPEEDS = [0, 1, 2, 5, 10, 20, 50, 100]
const FREE = [0, 2, 5, 10, 20, 50]
const STALL_HOURS = [1, 2, 3, 6, 12, 24, 48, 72, 0]
const HOURS = Array.from({ length: 24 }, (_, h) => h)

// A download speed limit (all day, or only between two hours) and how much
// free space to keep. Each change saves by itself.
function SpeedAndSpaceCard() {
  const limit = useAutosaveSetting<number>(
    (s) => s.speedLimitMB ?? 0,
    (v) => ({ speedLimitMB: v }),
    (v) => (v === 0 ? t('Saved: no speed limit.') : t('Saved: downloads are limited to {n} MB/s.', { n: v })),
    0,
  )
  const hours = useAutosaveSetting<string>(
    (s) => s.speedLimitHours ?? '',
    (v) => ({ speedLimitHours: v }),
    (v) => (v === '' ? t('Saved: the limit applies all day.') : t('Saved: the limit applies from {from}:00 to {to}:00.', { from: v.split('-')[0], to: v.split('-')[1] })),
    '',
  )
  const free = useAutosaveSetting<number>(
    (s) => s.minFreeGB ?? 5,
    (v) => ({ minFreeGB: v }),
    (v) => (v === 0 ? t('Saved: downloads start whatever the free space.') : t('Saved: no new download starts with less than {n} GB free.', { n: v })),
    5,
  )
  const dlWindow = useAutosaveSetting<string>(
    (s) => s.downloadHours ?? '',
    (v) => ({ downloadHours: v }),
    (v) => (v === '' ? t('Saved: downloads start at any time.') : t('Saved: new downloads start from {from}:00 to {to}:00.', { from: v.split('-')[0], to: v.split('-')[1] })),
    '',
  )
  const [from, to] = hours.value ? hours.value.split('-').map(Number) : [8, 23]
  const [wFrom, wTo] = dlWindow.value ? dlWindow.value.split('-').map(Number) : [1, 7]
  const busy = !limit.loaded || limit.saving || hours.saving || free.saving || dlWindow.saving
  return (
    <div>
      <label className="inline-field">
        {tx('Limit download speed to {speed}', {
          speed: (
            <span className="tip-last">
              <select value={limit.value} disabled={busy} onChange={(e) => void limit.change(Number(e.target.value))}>
                {SPEEDS.map((n) => (
                  <option key={n} value={n}>
                    {n === 0 ? t('no limit') : `${n} MB/s`}
                  </option>
                ))}
              </select>
              <InfoTip text={t('Caps Usenet and torrents together. Pick hours below to apply it only part of the day, so downloads run at full speed at night.')} learnMore="downloads#speed-and-space" />
            </span>
          ),
        })}
      </label>
      {limit.value > 0 && (
        <div className="inline-field" style={{ marginTop: 10 }}>
          <label className="inline-field">
            <input type="checkbox" checked={hours.value !== ''} disabled={busy} onChange={(e) => void hours.change(e.target.checked ? '8-23' : '')} />
            {tx('Only from {from} to {to}', {
              from: (
                <select aria-label={t('From')} value={from} disabled={busy || hours.value === ''} onChange={(e) => void hours.change(`${e.target.value}-${to === Number(e.target.value) ? (to + 1) % 24 : to}`)}>
                  {HOURS.map((h) => (
                    <option key={h} value={h}>
                      {h}:00
                    </option>
                  ))}
                </select>
              ),
              to: (
                <select aria-label={t('To')} value={to} disabled={busy || hours.value === ''} onChange={(e) => void hours.change(`${from === Number(e.target.value) ? (from + 23) % 24 : from}-${e.target.value}`)}>
                  {HOURS.map((h) => (
                    <option key={h} value={h}>
                      {h}:00
                    </option>
                  ))}
                </select>
              ),
            })}
          </label>
        </div>
      )}
      <p className="field-hint">{t('Usenet and torrents share the limit. Outside the hours you choose, downloads run at full speed.')}</p>
      <div className="inline-field" style={{ marginTop: 14 }}>
        <label className="inline-field">
          <input type="checkbox" checked={dlWindow.value !== ''} disabled={busy} onChange={(e) => void dlWindow.change(e.target.checked ? '1-7' : '')} />
          {tx('Only start downloads from {from} to {to}', {
            from: (
              <select aria-label={t('Downloads start from')} value={wFrom} disabled={busy || dlWindow.value === ''} onChange={(e) => void dlWindow.change(`${e.target.value}-${wTo === Number(e.target.value) ? (wTo + 1) % 24 : wTo}`)}>
                {HOURS.map((h) => (
                  <option key={h} value={h}>
                    {h}:00
                  </option>
                ))}
              </select>
            ),
            to: (
              <select aria-label={t('Downloads start until')} value={wTo} disabled={busy || dlWindow.value === ''} onChange={(e) => void dlWindow.change(`${wFrom === Number(e.target.value) ? (wFrom + 23) % 24 : wFrom}-${e.target.value}`)}>
                {HOURS.map((h) => (
                  <option key={h} value={h}>
                    {h}:00
                  </option>
                ))}
              </select>
            ),
          })}
        </label>
      </div>
      <p className="field-hint">{t('Outside these hours new downloads wait in line and start when the time comes, for example to download only at night. A download that is already running finishes.')}</p>
      <label className="inline-field" style={{ marginTop: 14 }}>
        {tx('Keep at least {size} free in the downloads folder', {
          size: (
            <select value={free.value} disabled={busy} onChange={(e) => void free.change(Number(e.target.value))}>
              {FREE.map((n) => (
                <option key={n} value={n}>
                  {n === 0 ? t('no minimum') : `${n} GB`}
                </option>
              ))}
            </select>
          ),
        })}
      </label>
      <p className="field-hint">{t('With less free space than this, no new download starts. They wait and start by themselves once there is room.')}</p>
    </div>
  )
}

// How long a torrent may get no data before Mediarium gives up on it.
function StalledTorrentCard() {
  const stall = useAutosaveSetting<number>(
    (s) => s.torrentStallHours ?? -1,
    (v) => ({ torrentStallHours: v }),
    (v) => (v < 0 ? t('Saved: a torrent is given up on after the usual time.') : v === 0 ? t('Saved: Mediarium never gives up on a torrent that gets no data.') : plural(v, 'Saved: a torrent that gets no data for {n} hour is given up on.', 'Saved: a torrent that gets no data for {n} hours is given up on.')),
    -1,
  )
  return (
    <section className="card">
      <h2><TipLabel help="downloads#stalled-torrents">{t('Stalled torrents')}</TipLabel></h2>
      <label className="inline-field">
        {tx('Give up on a torrent that gets no data for {time}', {
          time: (
            <select value={stall.value} disabled={stall.saving} onChange={(e) => void stall.change(Number(e.target.value))}>
              <option value={-1}>{t('the usual time (2 hours, 6 with a download client)')}</option>
              {STALL_HOURS.map((n) => (
                <option key={n} value={n}>
                  {n === 0 ? t('never') : plural(n, '1 hour', '{n} hours')}
                </option>
              ))}
            </select>
          ),
        })}
      </label>
      <p className="field-hint">{t('A torrent that has got nothing for that long is removed, its release goes on the blocklist and Mediarium searches for another one.')}</p>
    </section>
  )
}

function serverLabel(s: UsenetServer): string {
  return s.priority === 0 ? t('Primary') : t('Backup {n}', { n: s.priority })
}

// "Find the right number": opens connections one by one to see how many the
// provider lets this login have, and offers to set that number.
function ConnectionProbe({ server, onApply }: { server: UsenetServer; onApply: (n: number) => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState<{ accepted: number; refused: boolean; message: string } | null>(null)
  async function run() {
    setBusy(true)
    setError('')
    setResult(null)
    try {
      setResult(await api.probeUsenetServer(server.id))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="server-probe">
      <button onClick={() => void run()} disabled={busy} title={t('Opens connections one by one to see how many your provider allows at once')}>
        {busy ? t('Trying connections…') : t('Find the right number')}
      </button>
      {error && <span className="error-text">{error}</span>}
      {result && (
        <span className="probe-result">
          {st(result.message)}
          {result.refused && result.accepted > 0 && result.accepted !== server.connections && (
            <button className="primary btn-sm" onClick={() => void onApply(result.accepted)}>
              {t('Use {n}', { n: result.accepted })}
            </button>
          )}
        </span>
      )}
    </div>
  )
}

function UsenetServersSection({ onChange }: { onChange: () => void }) {
  const confirm = useConfirm()
  const [servers, setServers] = useState<UsenetServer[] | null>(null)
  const [editing, setEditing] = useState<UsenetServerDraft | null>(null)
  const toast = useToast()
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    api
      .listUsenetServers()
      .then(setServers)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    onChange()
  }, [onChange])
  useEffect(reload, [reload])
  useLive(reload, 15000)

  async function save(d: UsenetServerDraft) {
    setError('')
    try {
      const body = {
        name: d.name,
        host: d.host.trim(),
        port: d.port,
        useSsl: d.useSsl,
        username: d.username,
        password: d.password,
        connections: d.connections,
        priority: d.priority,
        enabled: d.enabled,
      }
      if (d.id !== undefined) await api.updateUsenetServer(d.id, body)
      else await api.createUsenetServer(body)
      setEditing(null)
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function setConnections(s: UsenetServer, n: number) {
    try {
      await api.updateUsenetServer(s.id, { ...s, password: '', connections: n })
      toast.success(t('{name} now uses {n} connections.', { name: s.name, n }))
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggle(s: UsenetServer) {
    try {
      await api.updateUsenetServer(s.id, { ...s, password: '', enabled: !s.enabled })
      toast.success(s.enabled ? t('{name} disabled.', { name: s.name }) : t('{name} enabled.', { name: s.name }))
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function remove(s: UsenetServer) {
    if (!(await confirm({ title: t('Remove the server "{name}"?', { name: s.name }), body: <p>{t('Downloads stop using it. You can add it again later.')}</p>, confirmLabel: t('Remove server'), danger: true }))) return
    try {
      await api.deleteUsenetServer(s.id)
      reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const edit = (s: UsenetServer): UsenetServerDraft => ({
    id: s.id,
    name: s.name,
    host: s.host,
    port: s.port,
    useSsl: s.useSsl,
    username: s.username ?? '',
    password: '',
    connections: s.connections,
    priority: s.priority,
    enabled: s.enabled,
    hasPassword: s.hasPassword,
  })

  return (
    <section className="card">
      <h2><TipLabel help="downloads#usenet-connections">{t('Your Usenet provider')}</TipLabel></h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {tx('The login your Usenet provider gave you. Add a second provider as a {backup}, ideally on a different backbone, to fill in articles the first one is missing.', { backup: <em>{t('backup')}</em> })}
      </p>
      {error && <p className="error-text">{error}</p>}

      {servers !== null && servers.length === 0 && !editing && (
        <p>
          <strong>{t('No Usenet provider yet.')}</strong> {t("NZB downloads need one, torrents don't.")}
        </p>
      )}
      {servers !== null && servers.length > 0 && (
        <div className="server-list">
          {servers.map((s) => (
            <div key={s.id} className="server-row" style={s.enabled ? undefined : { opacity: 0.6 }}>
              <div className="server-info">
                <div className="server-name">
                  <strong>{s.name}</strong>
                  <span className={`badge ${s.priority === 0 ? 'downloaded' : ''}`}>{serverLabel(s)}</span>
                  {!s.enabled && <span className="badge">{t('disabled')}</span>}
                </div>
                <div className="server-host">
                  {s.host}:{s.port} {s.useSsl ? '(SSL)' : t('(no SSL)')} · {plural(s.connections, '{n} connection', '{n} connections')}
                </div>
                {s.secretError && <div className="error-text">{st(s.secretError)}</div>}
                {s.enabled && !s.secretError && <ConnectionProbe server={s} onApply={(n) => setConnections(s, n)} />}
              </div>
              <div className="server-actions">
                <TestButton run={() => api.testUsenetServer(s.id)} />
                <button onClick={() => setEditing(edit(s))}>{t('Edit')}</button>
                <button onClick={() => toggle(s)}>{s.enabled ? t('Disable') : t('Enable')}</button>
                <button onClick={() => remove(s)}>{t('Remove')}</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {editing ? (
        <div>
          <h3 style={{ marginTop: 0 }}>{editing.id !== undefined ? (editing.name ? t('Edit {name}', { name: editing.name }) : t('Edit server')) : t('Add your Usenet provider')}</h3>
          <UsenetServerForm
            key={editing.id ?? 'new'}
            initial={editing}
            submitLabel={editing.id !== undefined ? t('Save server') : t('Add server')}
            onSubmit={save}
            onCancel={() => setEditing(null)}
          />
        </div>
      ) : (
        <button className="primary" onClick={() => setEditing(blankServer(servers && servers.length > 0 ? Math.max(...servers.map((s) => s.priority)) + 1 : 0))}>
          {servers && servers.length > 0 ? t('Add a backup provider…') : t('Add your Usenet provider…')}
        </button>
      )}
    </section>
  )
}
