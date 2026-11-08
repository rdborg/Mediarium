import { createPortal } from 'react-dom'
import { sortProfiles } from './qualityBlurb'
import { useEffect, useRef, useState } from 'react'
import { api, can, isAdmin, isRequested, isViewer, type DashboardData, type QualityProfile, type RequestStatus, type SourcePref } from '../api'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import type { NewTitleFolder } from '../api'
import { addChoices, mostRoomNote } from '../newTitleFolder'
import Icon from './Icon'
import ViewerNotice from './ViewerNotice'
import { TagChips, TagInput } from './Tags'
import { PosterFallback } from './PosterCard'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'
import { t, st } from '../i18n'
import { defaultProfileFor } from '../profileDefaults'
import InfoTip from './InfoTip'
import { collectionBodyFor, offersCollectionAdd } from '../collectionAdd'

export interface AddTarget {
  kind: 'movie' | 'tv'
  tmdbId: number
  title: string
  year?: number
  posterUrl?: string
  overview?: string
}

// Whether a new title keeps being searched for better versions after it is
// downloaded. Off unless the person turns it on; remembered in this browser.
const BETTER_KEY = 'mediarium.add.keepLookingForBetter'

function savedBetter(): boolean {
  try {
    return localStorage.getItem(BETTER_KEY) === '1'
  } catch {
    return false
  }
}

function saveBetter(on: boolean): void {
  try {
    localStorage.setItem(BETTER_KEY, on ? '1' : '0')
  } catch {
    // Storage can be blocked (private windows): the choice then lasts until the dialog closes.
  }
}

const SOURCE_LABEL: Record<Exclude<SourcePref, ''>, string> = {
  both: t('Usenet and torrents'),
  usenet: t('Usenet only'),
  torrent: t('Torrents only'),
}

// The "add to library" step, like Radarr's and Sonarr's: pick the quality
// profile, whether to monitor it (for a show: which episodes), which
// downloaders it may use, and whether to start looking for a release right
// away. Anything left on "default" follows Settings.
export default function AddDialog(props: { target: AddTarget; onClose: () => void; onAdded: (libraryId: number) => void }) {
  if (isViewer(useAuth().user)) return <ViewerNotice onClose={props.onClose} />
  return <AddForm {...props} />
}

function AddForm({
  target,
  onClose,
  onAdded,
}: {
  target: AddTarget
  onClose: () => void
  onAdded: (libraryId: number) => void
}) {
  const asks = !can(useAuth().user, 'addDirect')
  const toast = useToast()
  const isMovie = target.kind === 'movie'
  const { on } = useModules()
  const animeOn = !isMovie && on('anime') && on('tv')
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [defaultProfileId, setDefaultProfileId] = useState(0)
  const [defaultSources, setDefaultSources] = useState<SourcePref>('both')
  const [dash, setDash] = useState<Pick<DashboardData['setup'], 'indexers' | 'usenetServers'> | null>(null)
  const [profileId, setProfileId] = useState(0)
  const [monitored, setMonitored] = useState(true)
  const [monitor, setMonitor] = useState<'all' | 'future' | 'none'>('all')
  const [sources, setSources] = useState<SourcePref>('')
  const [searchNow, setSearchNow] = useState(true)
  // The library folders to choose from, when more than one is set up (the main one first).
  const [folders, setFolders] = useState<string[]>([])
  const [rootPath, setRootPath] = useState('')
  const [newNow, setNewNow] = useState<NewTitleFolder | undefined>()
  const [tags, setTags] = useState<string[]>([])
  const [knownTags, setKnownTags] = useState<string[]>([])
  useEffect(() => {
    api
      .listTags()
      .then((l) => setKnownTags(l.map((x) => x.name)))
      .catch(() => undefined)
  }, [])
  const [keepBetter, setKeepBetter] = useState(savedBetter)
  // The TMDB collection this film belongs to, if any (administrators can add the rest of it too).
  const admin = isAdmin(useAuth().user)
  const [collection, setCollection] = useState<{ id: number; name: string } | null>(null)
  const [addCollection, setAddCollection] = useState(false)
  useEffect(() => {
    if (!isMovie || !admin || asks) return
    api
      .tmdbMovieDetail(target.tmdbId)
      .then((d) => setCollection(d.collection ? { id: d.collection.id, name: d.collection.name } : null))
      .catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const collectionOffered = offersCollectionAdd(isMovie, admin, asks, collection?.id)
  // For an account that has to ask: can it ask for the 4K copy too, and does it want to.
  const [reqStatus, setReqStatus] = useState<RequestStatus | null>(null)
  const [wantFourK, setWantFourK] = useState(false)
  useEffect(() => {
    if (asks) api.requestStatus().then(setReqStatus).catch(() => undefined)
  }, [asks])
  const fourKOffered = asks && !!reqStatus && (target.kind === 'movie' ? reqStatus.fourK.movie : reqStatus.fourK.tv)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)

  useEffect(() => {
    api.listProfiles().then((r) => {
      setProfiles(sortProfiles(r.profiles))
      setDefaultProfileId(defaultProfileFor(target.kind, r))
      setDefaultSources(r.defaultSources || 'both')
    }).catch(() => undefined)
    api
      .libraryFolders()
      .then((f) => {
        const list = target.kind === 'movie' ? f.movies : f.tv
        const now = target.kind === 'movie' ? f.newTitles.movies : f.newTitles.tv
        setFolders(list)
        setNewNow(now)
        setRootPath(addChoices(list, now).initial)
      })
      .catch(() => undefined)
    api.dashboard().then((d) => setDash(d.setup)).catch(() => undefined)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  const defaultProfile = profiles.find((p) => p.id === defaultProfileId)
  const effectiveSources = sources || defaultSources
  const cannotSearch = dash !== null && dash.indexers === 0

  async function submit() {
    setBusy(true)
    setError('')
    try {
      let libraryId: number
      const res = isMovie
        ? await api.addMovie(target.tmdbId, { profileId: profileId || undefined, monitored, sources, searchNow: searchNow && !cannotSearch, noUpgrade: !keepBetter, rootPath: rootPath || undefined, fourK: fourKOffered && wantFourK ? true : undefined })
        : await api.addSeries(target.tmdbId, { profileId: profileId || undefined, monitor, sources, searchNow: searchNow && !cannotSearch, noUpgrade: !keepBetter, rootPath: rootPath || undefined, fourK: fourKOffered && wantFourK ? true : undefined })
      if (isRequested(res)) {
        toast.success(st(res.message))
        onClose()
        return
      }
      libraryId = res.id
      if (tags.length > 0) {
        await api.setTitleTags(isMovie ? 'movie' : 'tv', libraryId, tags).catch(() => undefined)
      }
      toast.success(searchNow && !cannotSearch ? t('{title} added. Searching for a release now.', { title: target.title }) : t('{title} added.', { title: target.title }))
      if (collectionOffered && addCollection && collection) {
        // The movie is in the library now, so this adds the other films with the same choices.
        try {
          const r = await api.addCollection(collection.id, collectionBodyFor({ profileId, rootPath, monitored, sources, searchNow: searchNow && !cannotSearch }))
          toast.success(st(r.message))
        } catch (e) {
          toast.error(e instanceof Error ? e.message : String(e))
        }
      }
      onAdded(libraryId)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={t('Add {name}', { name: target.title })}>
        <div className="modal-head">
          {target.posterUrl ? <img src={target.posterUrl} alt="" /> : <PosterFallback />}
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>
              {target.title} {target.year ? <span style={{ color: 'var(--text-dim)', fontWeight: 400 }}>({target.year})</span> : null}
            </h2>
            <span className="badge">{isMovie ? t('Movie') : t('TV show')}</span>
            {target.overview && (
              <p style={{ color: 'var(--text-dim)', fontSize: '0.88rem', display: '-webkit-box', WebkitLineClamp: 3, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>
                {target.overview}
              </p>
            )}
          </div>
          <button className="icon-btn" onClick={onClose} disabled={busy} aria-label={t('Close')}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          <label>
            <span className="has-tip">
              {t('Quality profile')}
              <InfoTip text={t('Decides which releases are good enough to download. "Default" uses the profile that is set as your default.')} />
            </span>
            <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))}>
              <option value={0}>{defaultProfile ? t('Default ({name})', { name: defaultProfile.name }) : t('Default')}</option>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            {animeOn && profileId === 0 && <small className="field-hint">{t("An anime show left on Default gets the anime library's profile and goes to its folder.")}</small>}
          </label>

          {folders.length > 1 && (
            <label>
              <span className="has-tip">
                {t('Keep it in')}
                <InfoTip text={t('The library folder this title is stored in once it is downloaded.')} />
              </span>
              <select value={rootPath} onChange={(e) => setRootPath(e.target.value)}>
                {addChoices(folders, newNow).options.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
              {mostRoomNote(newNow) && <small className="path-note">{mostRoomNote(newNow)}</small>}
            </label>
          )}

          {isMovie ? (
            <label>
              <span className="has-tip">
                {t('Monitoring')}
                <InfoTip text={t('A monitored movie is searched for and downloaded by itself. If you choose "Not monitored" it only goes into your library; nothing is searched for or downloaded until you do it yourself.')} />
              </span>
              <select value={monitored ? 'yes' : 'no'} onChange={(e) => setMonitored(e.target.value === 'yes')}>
                <option value="yes">{t('Monitored: search for it and download it')}</option>
                <option value="no">{t('Not monitored: just add it to the library')}</option>
              </select>
            </label>
          ) : (
            <label>
              <span className="has-tip">
                {t('Which episodes to monitor')}
                <InfoTip text={t('Only monitored episodes are searched for and downloaded. "None" adds the show to your library and downloads nothing; you can switch episodes on later.')} />
              </span>
              <select value={monitor} onChange={(e) => setMonitor(e.target.value as 'all' | 'future' | 'none')}>
                <option value="all">{t('All episodes')}</option>
                <option value="future">{t("Only episodes that haven't aired yet")}</option>
                <option value="none">{t('None (add it, download nothing)')}</option>
              </select>
            </label>
          )}

          <div className="add-tags">
            <span className="has-tip">
              {t('Tags (optional)')}
              <InfoTip text={t('Your own labels, such as Kids or 4K. You can filter the Library by tag, and on a connected media server each tag becomes a collection.')} />
            </span>
            <TagChips tags={tags} onRemove={(x) => setTags((cur) => cur.filter((y) => y !== x))} />
            <TagInput
              suggestions={knownTags.filter((k) => !tags.some((x) => x.toLowerCase() === k.toLowerCase()))}
              onAdd={(x) => setTags((cur) => (cur.some((y) => y.toLowerCase() === x.toLowerCase()) ? cur : [...cur, x]))}
              placeholder={t('Like Kids or 4K')}
              disabled={busy}
            />
          </div>

          <label>
            <span className="has-tip">
              {t('Download from')}
              <InfoTip text={t('Which downloaders may fetch this title. "Default" follows your setting; pick Usenet only or Torrents only to rule the other one out for this title.')} />
            </span>
            <select value={sources} onChange={(e) => setSources(e.target.value as SourcePref)}>
              <option value="">{t('Default ({name})', { name: SOURCE_LABEL[defaultSources as Exclude<SourcePref, ''>] ?? SOURCE_LABEL.both })}</option>
              <option value="both">{SOURCE_LABEL.both}</option>
              <option value="usenet">{SOURCE_LABEL.usenet}</option>
              <option value="torrent">{SOURCE_LABEL.torrent}</option>
            </select>
          </label>

          <label style={{ flexDirection: 'row', alignItems: 'center', gap: 8, color: 'var(--text)' }}>
            <input type="checkbox" checked={searchNow} onChange={(e) => setSearchNow(e.target.checked)} disabled={cannotSearch} />
            <span className="has-tip">
              {isMovie ? t('Start searching for it now') : t('Start searching for missing episodes now')}
              <InfoTip text={t('Looks for a release as soon as you add it. If you leave it unticked, the title waits for the next automatic search or for you to press Search now. It is greyed out until you add an indexer.')} />
            </span>
          </label>

          <label className="check-row" style={{ color: 'var(--text)' }}>
            <input
              type="checkbox"
              checked={keepBetter}
              onChange={(e) => {
                setKeepBetter(e.target.checked)
                saveBetter(e.target.checked)
              }}
            />
            <span>
              <span className="has-tip">
                {t('Keep looking for better versions after it is downloaded')}
                <InfoTip text={t('Mediarium swaps the file for a better release when your quality profile allows it. Left unticked, the title stays as first downloaded.')} />
              </span>
              <small style={{ display: 'block', color: 'var(--text-dim)', fontWeight: 400 }}>
                {t('You can change this on its page later.')}
              </small>
            </span>
          </label>

          {collectionOffered && collection && (
            <label className="check-row" style={{ color: 'var(--text)' }}>
              <input type="checkbox" checked={addCollection} onChange={(e) => setAddCollection(e.target.checked)} disabled={busy} />
              <span>
                <span className="has-tip">
                  {t('Add the other films of this collection too')}
                  <InfoTip text={t('Adds the rest of the collection with the same quality profile, folder, monitoring and search choice as this film. Films you already have are left alone.')} />
                </span>
                <small style={{ display: 'block', color: 'var(--text-dim)', fontWeight: 400 }}>{collection.name}</small>
              </span>
            </label>
          )}

          {fourKOffered && (
            <label className="check-row" style={{ color: 'var(--text)' }}>
              <input type="checkbox" checked={wantFourK} onChange={(e) => setWantFourK(e.target.checked)} />
              <span>
                <span className="has-tip">
                  {t('Ask for a 4K copy as well')}
                  <InfoTip text={t('Also asks for a second copy of the title in 4K, kept next to the normal one. An administrator still has to approve it.')} />
                </span>
                <small style={{ display: 'block', color: 'var(--text-dim)', fontWeight: 400 }}>
                  {reqStatus?.fourKLabel ? t('After the title is added, a second copy in {quality} is added too.', { quality: reqStatus.fourKLabel }) : t('After the title is added, a second copy in 4K is added too.')}
                </small>
              </span>
            </label>
          )}

          {cannotSearch && (
            <div className="notice notice-warn">
              <strong>{t('No indexer yet.')}</strong> {t('It will be added and searched once you add one (Settings > Indexers & Search).')}
            </div>
          )}
          {dash && dash.indexers > 0 && effectiveSources !== 'torrent' && dash.usenetServers === 0 && (
            <div className="notice notice-warn">
              {t("Add your Usenet provider's login to download from Usenet (Settings > Downloading > Usenet).")}
            </div>
          )}
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>{t('Cancel')}</button>
          <button className="primary btn-with-icon" onClick={submit} disabled={busy}>
            <Icon name="plus" size={16} /> {busy ? (asks ? t('Sending…') : t('Adding…')) : asks ? (isMovie ? t('Request movie') : t('Request show')) : isMovie ? t('Add movie') : t('Add show')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
