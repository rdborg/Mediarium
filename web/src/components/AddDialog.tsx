import { createPortal } from 'react-dom'
import { sortProfiles } from './qualityBlurb'
import { useEffect, useState } from 'react'
import { api, type DashboardData, type QualityProfile, type SourcePref } from '../api'
import Icon from './Icon'
import { PosterFallback } from './PosterCard'
import { useToast } from './Toast'

export interface AddTarget {
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  overview?: string
}

const SOURCE_LABEL: Record<Exclude<SourcePref, ''>, string> = {
  both: 'Usenet and torrents',
  usenet: 'Usenet only',
  torrent: 'Torrents only',
}

// The "add to library" step, like Radarr's and Sonarr's: pick the quality
// profile, whether to monitor it (for a show: which episodes), which
// downloaders it may use, and whether to start looking for a release right
// away. Anything left on "default" follows Settings.
export default function AddDialog({
  target,
  onClose,
  onAdded,
}: {
  target: AddTarget
  onClose: () => void
  onAdded: (libraryId: number) => void
}) {
  const toast = useToast()
  const isMovie = target.kind === 'movie'
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfileId, setDefaultProfileId] = useState(0)
  const [defaultSources, setDefaultSources] = useState<SourcePref>('both')
  const [dash, setDash] = useState<Pick<DashboardData['setup'], 'indexers' | 'usenetServers'> | null>(null)
  const [profileId, setProfileId] = useState(0)
  const [monitored, setMonitored] = useState(true)
  const [monitor, setMonitor] = useState<'all' | 'future' | 'none'>('all')
  const [sources, setSources] = useState<SourcePref>('')
  const [searchNow, setSearchNow] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(sortProfiles(r.profiles))
      setDefaultProfileId(r.defaultId)
    }).catch(() => undefined)
    api.getSettings().then((s) => setDefaultSources(s.defaultSources ?? 'both')).catch(() => undefined)
    api.dashboard().then((d) => setDash(d.setup)).catch(() => undefined)
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const defaultProfile = profiles.find((p) => p.id === defaultProfileId)
  const effectiveSources = sources || defaultSources
  const cannotSearch = dash !== null && dash.indexers === 0

  async function submit() {
    setBusy(true)
    setError('')
    try {
      let libraryId: number
      if (isMovie) {
        const m = await api.addMovie(target.tmdbId, { profileId: profileId || undefined, monitored, sources, searchNow: searchNow && !cannotSearch })
        libraryId = m.id
      } else {
        const s = await api.addSeries(target.tmdbId, { profileId: profileId || undefined, monitor, sources, searchNow: searchNow && !cannotSearch })
        libraryId = s.id
      }
      toast.success(`${target.title} added${searchNow && !cannotSearch ? ' — searching for a release now.' : '.'}`)
      onAdded(libraryId)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal" role="dialog" aria-modal="true" aria-label={`Add ${target.title}`}>
        <div className="modal-head">
          {target.posterUrl ? <img src={target.posterUrl} alt="" /> : <PosterFallback />}
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>
              {target.title} {target.year ? <span style={{ color: 'var(--text-dim)', fontWeight: 400 }}>({target.year})</span> : null}
            </h2>
            <span className="badge">{isMovie ? 'Movie' : 'TV show'}</span>
            {target.overview && (
              <p style={{ color: 'var(--text-dim)', fontSize: '0.88rem', display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
                {target.overview}
              </p>
            )}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label="Close">
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          <label>
            Quality profile
            <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
              <option value={0}>Default{defaultProfile ? ` (${defaultProfile.name})` : ''}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>

          {isMovie ? (
            <label>
              Monitoring
              <select value={monitored ? 'yes' : 'no'} onChange={(e) => setMonitored(e.target.value === 'yes')}>
                <option value="yes">Monitored: search for it and keep it upgraded</option>
                <option value="no">Not monitored: just add it to the library</option>
              </select>
            </label>
          ) : (
            <label>
              Which episodes to monitor
              <select value={monitor} onChange={(e) => setMonitor(e.target.value as 'all' | 'future' | 'none')}>
                <option value="all">All episodes</option>
                <option value="future">Only episodes that haven't aired yet</option>
                <option value="none">None (add it, download nothing automatically)</option>
              </select>
            </label>
          )}

          <label>
            Download from
            <select value={sources} onChange={(e) => setSources(e.target.value as SourcePref)}>
              <option value="">Default ({SOURCE_LABEL[defaultSources as Exclude<SourcePref, ''>] ?? 'Usenet and torrents'})</option>
              <option value="both">{SOURCE_LABEL.both}</option>
              <option value="usenet">{SOURCE_LABEL.usenet}</option>
              <option value="torrent">{SOURCE_LABEL.torrent}</option>
            </select>
          </label>

          <label style={{ flexDirection: 'row', alignItems: 'center', gap: 8, color: 'var(--text)' }}>
            <input type="checkbox" checked={searchNow} onChange={(e) => setSearchNow(e.target.checked)} disabled={cannotSearch} />
            Start searching for {isMovie ? 'it' : 'missing episodes'} now
          </label>

          {cannotSearch && (
            <div className="notice notice-warn">
              <strong>No indexer is set up yet,</strong> so nothing can be searched. It will be added, and searched once you add an indexer (Settings &gt; Indexers).
            </div>
          )}
          {dash && dash.indexers > 0 && effectiveSources !== 'torrent' && dash.usenetServers === 0 && (
            <div className="notice notice-warn">
              Usenet downloads need your provider's login, which isn't added yet (Settings &gt; Downloads &amp; VPN).
            </div>
          )}
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose}>Cancel</button>
          <button className="primary btn-with-icon" onClick={submit} disabled={busy}>
            <Icon name="plus" size={16} /> {busy ? 'Adding…' : `Add ${isMovie ? 'movie' : 'show'}`}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
