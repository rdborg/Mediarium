import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import Icon from '../components/Icon'
import InfoTip from '../components/InfoTip'
import ProfilePicker from '../components/ProfilePicker'
import TitleTags from '../components/Tags'
import WatchedLine from '../components/WatchedLine'
import NumberingCard from '../components/NumberingCard'
import SeriesTypePicker from '../components/SeriesTypePicker'
import FileChips from '../components/FileChips'
import ShortFile from '../components/ShortFile'
import { redoAnswerText } from '../shortFile'
import Switch from '../components/Switch'
import MonitorModePicker from '../components/MonitorModePicker'
import TitleHero from '../components/TitleHero'
import WatchLinks from '../components/WatchLinks'
import FolderLine from '../components/FolderLine'
import LocateDialog from '../components/LocateDialog'
import TitleEvents from '../components/TitleEvents'
import { useToast } from '../components/Toast'
import { useTorrentsSwitch } from '../useTorrentsSwitch'
import { isTorrentsOff } from '../torrentsOff'
import ReleaseTable from '../components/ReleaseTable'
import GrabBlocked from '../components/GrabBlocked'
import { blockingOf, type BlockingDownload } from '../grabBlocked'
import CopiesCard from '../components/CopiesCard'
import TitleRequestActions from '../components/TitleRequestActions'
import FilesPanel from '../components/FilesPanel'
import SubtitlesOffNote from '../components/SubtitlesOffNote'
import SubtitlesPanel from '../components/SubtitlesPanel'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { api, can, isAdmin, isViewer, type Episode, type SearchResult, type SeriesDetail as SeriesDetailData, type TVDetail } from '../api'
import { formatBytes, qualityText, withNameTag } from '../format'
import { showStatusLabel } from '../showStatus'
import { rescanMessage } from '../rescanMessage'
import { CastCard, PeopleLinks, titleLinks } from '../components/TitlePreview'
import { crewWith } from '../people'
import { useLive } from '../useLive'
import { useConfirm } from '../components/ConfirmProvider'
import { removeOption } from '../diskUsage'
import CustomSearch from '../components/CustomSearch'
import ReleaseNotice from '../components/ReleaseNotice'
import type { Unanswered } from '../releaseHint'
import type { Searched } from '../searched'
import { useDocumentTitle } from '../documentTitle'
import { localDate } from '../bookSeries'
import { entryDay, entryTime, readZonePref, resolveZone } from '../airTime'
import { locale, plural, t } from '../i18n'
import { monitoringSummary, readSeasonsOpen, rememberSeasonsOpen, seasonStartsOpen, type SeasonsOpen } from '../seriesView'
import { sumBytes } from '../diskTotals'
import { episodeFilesToReplace, replaceQuestion } from '../replaceFile'

// A target for interactive search: one episode, or a whole season (no
// episode set) which only offers season packs.
interface SearchTarget {
  season: number
  episode?: number
  label: string
}

// What an episode's status badge says; the status itself stays the key.
const EP_STATUS: Record<string, string> = {
  missing: t('missing'),
  downloading: t('downloading'),
  downloaded: t('downloaded'),
}

// Series detail: seasons with per-episode status, and Sonarr-style
// interactive search — pick an episode (or a whole season) and choose which
// release to grab.
export default function SeriesDetail() {
  // Another show starts clean, so nothing of the last one (its releases, its messages) stays on the page.
  return <SeriesPage key={useParams().id} />
}

function SeriesPage() {
  const confirm = useConfirm()
  const me = useAuth().user
  const admin = isAdmin(me)
  // What this account may do: a viewer, or a basic account with these switched
  // off, is not shown buttons the server would refuse (as on a movie's page).
  const canManage = can(me, 'manage')
  const canReleases = can(me, 'releases')
  const viewer = isViewer(me)
  const { subtitlesOn } = useModules()
  const { id } = useParams()
  const seriesId = Number(id)
  const navigate = useNavigate()
  const [series, setSeries] = useState<SeriesDetailData | null>(null)
  useDocumentTitle(series?.title)
  const [tv, setTv] = useState<TVDetail | null>(null)
  const [error, setError] = useState('')
  // A refresh that fails while the show is already on screen is not worth an error message.
  const [loadError, setLoadError] = useState('')
  const [message, setMessage] = useState('')
  const [target, setTarget] = useState<SearchTarget | null>(null)
  const [results, setResults] = useState<SearchResult[] | null>(null)
  const [answered, setAnswered] = useState<{ sources: number; unanswered: Unanswered[]; warnings?: string[]; searched?: Searched[] }>({ sources: 0, unanswered: [] })
  const [searching, setSearching] = useState(false)
  const [grabbing, setGrabbing] = useState<string | null>(null)
  const [searchingNow, setSearchingNow] = useState(false)
  const [subsOpen, setSubsOpen] = useState<number | null>(null)
  // Which seasons are open (unset: only the one that needs attention), and the
  // "Missing episodes only" switch.
  const [openSeasons, setOpenSeasons] = useState<Record<number, boolean>>({})
  // How the seasons start (all folded, the latest open, all open): this browser's choice.
  const [seasonsMode, setSeasonsMode] = useState<SeasonsOpen>(() => readSeasonsOpen())
  function chooseSeasonsMode(mode: SeasonsOpen) {
    setSeasonsMode(mode)
    rememberSeasonsOpen(mode)
    setOpenSeasons({})
  }
  const [missingOnly, setMissingOnly] = useState(false)
  const toast = useToast()
  const { grabError } = useTorrentsSwitch()

  const load = useCallback(() => {
    api
      .getSeries(seriesId)
      .then((s) => {
        setSeries(s)
        setLoadError('')
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : String(e)))
  }, [seriesId])
  useEffect(load, [load])
  const tmdbId = series?.tmdbId
  useEffect(() => {
    if (tmdbId) api.tmdbTVDetail(tmdbId).then(setTv).catch(() => undefined)
  }, [tmdbId])

  // Refreshes itself, faster while anything is downloading so statuses update without a reload.
  const anyDownloading = series?.episodes.some((e) => e.status === 'downloading') ?? false
  useLive(load, anyDownloading ? 3000 : 10000)

  const seasons = useMemo(() => {
    const map = new Map<number, Episode[]>()
    for (const e of series?.episodes ?? []) {
      map.set(e.season, [...(map.get(e.season) ?? []), e])
    }
    return [...map.entries()].sort((a, b) => a[0] - b[0])
  }, [series])

  // The releases list sits above the seasons: bring it into view when a search starts.
  const releasesRef = useRef<HTMLElement>(null)
  const searchOpen = target !== null
  useEffect(() => {
    if (searchOpen) releasesRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [searchOpen, target?.label])

  // Only the answer to the latest search is shown, so a slow earlier one can't land under the wrong episode.
  const searchId = useRef(0)
  async function runSearch(st: SearchTarget, q?: string) {
    const mine = ++searchId.current
    setTarget(st)
    setResults(null)
    setSearching(true)
    setError('')
    setMessage('')
    try {
      const a = await api.seriesSearch(seriesId, st.season, st.episode, q)
      if (mine !== searchId.current) return
      setAnswered({ sources: a.sources, unanswered: a.unanswered, warnings: a.warnings, searched: a.searched })
      setResults(a.results)
    } catch (e) {
      if (mine === searchId.current) setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (mine === searchId.current) setSearching(false)
    }
  }

  // A grab that was refused because another download is in the way, with the
  // release that was picked, so it can be repeated once that one is cancelled.
  const [blocked, setBlocked] = useState<{ release: SearchResult; blocking: BlockingDownload; message: string; replace: boolean } | null>(null)

  // replaceFiles is what the person already decided about the files the
  // episodes have (when the grab is repeated); left out, episodes that have
  // files ask first.
  async function grab(r: SearchResult, replaceQueueId?: number, replaceFiles?: boolean) {
    let replace = replaceFiles ?? false
    if (replaceFiles === undefined) {
      const old = episodeFilesToReplace(series?.episodes ?? [], { season: r.season, episodes: r.episodes }, target)
      if (old.length > 0) {
        const q = replaceQuestion(old, r)
        const answer = await confirm({
          title: q.title,
          body: q.lines.map((line) => (
            <p key={line} className="confirm-break">
              {line}
            </p>
          )),
          confirmLabel: t('Replace'),
          cancelLabel: t('Cancel'),
        })
        if (!answer) return
        replace = true
      }
    }
    setGrabbing(r.downloadUrl)
    setError('')
    setBlocked(null)
    try {
      await api.grabSeries(seriesId, {
        releaseTitle: r.title,
        downloadUrl: r.downloadUrl,
        sizeBytes: r.sizeBytes,
        protocol: r.protocol,
        season: target?.season,
        episode: target?.episode,
        replaceQueueId,
        replace: replace || undefined,
      })
      setMessage(t('Started downloading "{title}". Follow it in Activity.', { title: r.title }))
      setTimeout(load, 500)
    } catch (e) {
      const blocking = blockingOf(e)
      if (blocking) setBlocked({ release: r, blocking, message: e instanceof Error ? e.message : String(e), replace })
      else if (isTorrentsOff(e)) grabError(e, () => void grab(r, replaceQueueId, replace))
      else setError(e instanceof Error ? e.message : String(e))
    } finally {
      setGrabbing(null)
    }
  }

  async function toggleSeries(monitored: boolean) {
    try {
      await api.setSeriesMonitored(seriesId, monitored)
      toast.success(monitored ? t('Monitored: looking for missing episodes.') : t('Unmonitored: this show is left alone.'))
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggleSeason(season: number, monitored: boolean) {
    try {
      await api.setSeasonMonitored(seriesId, season, monitored)
      toast.success(monitored ? t('Season {n} monitored.', { n: season }) : t('Season {n} unmonitored.', { n: season }))
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function toggleEpisode(id: number, monitored: boolean) {
    try {
      await api.setEpisodeMonitored(id, monitored)
      toast.success(monitored ? t('Episode monitored.') : t('Episode unmonitored.'))
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function searchNow(scope: { season?: number; episode?: number } = {}) {
    setSearchingNow(true)
    setError('')
    setMessage('')
    try {
      const res = await api.searchNowSeries(seriesId, scope)
      setMessage(res.message)
      if (res.grabbed > 0) setTimeout(load, 500)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSearchingNow(false)
    }
  }

  const [refreshing, setRefreshing] = useState(false)
  const [rescanning, setRescanning] = useState(false)
  const [changingFolder, setChangingFolder] = useState(false)
  const [linking, setLinking] = useState<Episode | null>(null)
  async function rescan() {
    setError('')
    setRescanning(true)
    try {
      setMessage(rescanMessage(await api.rescanSeries(seriesId), series?.monitored !== false))
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRescanning(false)
    }
  }

  async function refresh() {
    setError('')
    setRefreshing(true)
    try {
      await api.refreshSeries(seriesId)
      setMessage(t('Episode list refreshed.'))
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRefreshing(false)
    }
  }

  // Delete the files of one episode or a whole season, keeping the show: the
  // episodes are wanted again, so a new release can take their place.
  async function deleteFiles(what: { episode: Episode } | { season: number }) {
    const one = 'episode' in what
    const label = one ? `S${String(what.episode.season).padStart(2, '0')}E${String(what.episode.episode).padStart(2, '0')}` : what.season === 0 ? t('Specials') : t('Season {n}', { n: what.season })
    const answer = await confirm({
      title: one ? t('Delete the file of {name}?', { name: label }) : t('Delete the files of {name}?', { name: label }),
      body: <p>{t('The files go to the recycle bin (or are deleted, if it is switched off). {title} stays in your library and these episodes are wanted again, so Mediarium can look for a new release.', { title: series?.title ?? '' })}</p>,
      confirmLabel: t('Delete'),
      danger: true,
    })
    if (!answer) return
    try {
      const r = one ? await api.deleteEpisodeFile(what.episode.id) : await api.deleteSeasonFiles(seriesId, what.season)
      setMessage(plural(r.episodes, 'Deleted the file. {n} episode is wanted again.', 'Deleted the files. {n} episodes are wanted again.'))
      load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  // An episode file far shorter than the episode should be: delete it and look for another release.
  async function redoShortFile(e: Episode) {
    const label = `S${String(e.season).padStart(2, '0')}E${String(e.episode).padStart(2, '0')}`
    const answer = await confirm({
      title: t('Delete the file of {name} and search again?', { name: label }),
      body: <p>{t('This file is much shorter than the episode should be, so it is most likely a fake or a cut-off download. It goes to the recycle bin (or is deleted, if that is switched off), the release it came from is blocklisted when Mediarium knows it, and Mediarium searches for another one.')}</p>,
      confirmLabel: t('Delete and search again'),
      danger: true,
    })
    if (!answer) return
    try {
      setMessage(redoAnswerText(await api.redoIncomplete({ episodeIds: [e.id] })))
      load()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  async function remove() {
    if (!series) return
    const usage = await api.seriesDiskUsage(seriesId).catch(() => null)
    const copies = series.copyOf ? [] : ((await api.titleCopies('series', seriesId).catch(() => null))?.items ?? [])
    const answer = await confirm({
      title: series.copyOf ? t('Remove this second copy of {title}?', { title: series.title }) : t('Remove {title} from your library?', { title: series.title }),
      body: (
        <p>
          {series.copyOf
            ? t('Mediarium stops tracking this copy and cancels anything still downloading for it. The main show isn’t touched.')
            : t('Mediarium stops tracking this show and cancels anything still downloading for it. You can add it back any time.')}
          {copies.length > 0 &&
            ` ${plural(copies.length, 'Its second copy ({names}) goes too, with its files if you tick the box.', 'Its second copies ({names}) go too, with their files if you tick the box.', { names: copies.map((c) => c.profileName).join(', ') })}`}
        </p>
      ),
      confirmLabel: t('Remove'),
      danger: true,
      option: removeOption('show', usage),
    })
    if (!answer) return
    const deleteFiles = answer.checked
    try {
      await api.deleteSeries(seriesId, deleteFiles)
      navigate('/library?kind=tv')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  if (!series) return loadError || error ? <p className="error-text">{loadError || error}</p> : <p>{t('Loading…')}</p>

  const creators = crewWith(tv?.crew, 'Creator')
  const today = localDate(new Date())
  const isMissing = (e: { status: string; airDate?: string }) => e.status === 'missing' && !!e.airDate && e.airDate <= today
  // The next episode still to air, for the facts at the top.
  const next = series.episodes.filter((e) => e.airDate && e.airDate >= today).sort((a, b) => (a.airDate! < b.airDate! ? -1 : 1))[0]
  // With an exact air time the day and hour are the viewer's own; without one, the date as TMDB has it.
  const nextZone = resolveZone(readZonePref())
  const nextEntry = next ? { kind: 'episode', releaseDate: next.airDate!, airAt: next.airAt } : null
  const nextText = next && nextEntry
    ? t('{episode} on {date}', {
        episode: `S${String(next.season).padStart(2, '0')}E${String(next.episode).padStart(2, '0')}`,
        date:
          new Date(entryDay(nextEntry, nextZone) + 'T12:00:00').toLocaleDateString(locale(), { day: 'numeric', month: 'short', year: 'numeric' }) +
          (entryTime(nextEntry, nextZone) ? `, ${entryTime(nextEntry, nextZone)}` : ''),
      })
    : ''
  // Unless the person chose otherwise (seasonsMode), seasons start folded,
  // except the one that needs attention: the latest with missing episodes, or
  // else the latest season.
  const attention = [...seasons].reverse().find(([, eps]) => eps.some(isMissing))?.[0] ?? seasons[seasons.length - 1]?.[0]
  const isOpen = (season: number) => openSeasons[season] ?? seasonStartsOpen(seasonsMode, season, attention)
  const monitoring = monitoringSummary(series.monitored, series.episodes, today)
  const diskBytes = sumBytes(series.episodes)
  const shownSeasons = missingOnly ? seasons.filter(([, eps]) => eps.some(isMissing)) : seasons

  return (
    <div>
      {series.copyOf ? (
        <div className="notice notice-info" style={{ marginBottom: 16 }}>
          {t('This is a second copy of the show, kept in another quality in the copies folder. Its episodes are looked for on their own.')}{' '}
          <Link to={`/series/${series.copyOf}`}>{t('Open the main show')}</Link>
        </div>
      ) : null}
      <TitleHero
        info={{
          ...(tv ?? {}),
          kind: 'tv',
          title: withNameTag(series.title, series.nameTag, series.year),
          year: series.year,
          posterUrl: series.posterUrl,
          overview: series.overview,
          certification: tv?.contentRating,
          links: titleLinks('tv', series.tmdbId, tv?.imdbId, tv?.homepage),
          facts: [
            ...(creators.length ? [{ label: creators.length > 1 ? t('Creators') : t('Creator'), value: <PeopleLinks people={creators} /> }] : []),
            ...(series.showStatus ? [{ label: t('Status'), value: showStatusLabel(series.showStatus) }] : []),
            { label: t('Episodes'), value: t('{done} of {total} downloaded', { done: series.downloadedCount, total: series.episodeCount }) },
            ...(diskBytes > 0 ? [{ label: t('Size on disk'), value: formatBytes(diskBytes) }] : []),
            ...(nextText ? [{ label: t('Next episode'), value: nextText }] : []),
            ...(tv?.networks?.length ? [{ label: t('Network'), value: tv.networks.join(', ') }] : []),
          ],
        }}
        status={
          <span className={`state-tag st-mon-${monitoring.kind === 'off' ? 'off' : 'on'}`} title={monitoring.detail}>
            <Icon name={monitoring.kind === 'off' ? 'eye-off' : 'eye'} size={13} /> {monitoring.text}
          </span>
        }
        actions={
          <>
            {canManage && (
              <button className="primary btn-with-icon" onClick={() => searchNow()} disabled={searchingNow}>
                <Icon name="search" size={16} /> {searchingNow ? t('Searching…') : t('Find missing episodes')}
              </button>
            )}
            {canManage && <InfoTip text={t('Searches your indexers right now for the episodes that are still missing, even if the show is not monitored. To search one season or one episode, use the buttons further down.')} />}
            {!viewer && (
              <button className="btn-with-icon" onClick={refresh} disabled={refreshing}>
                <Icon name="refresh" size={16} /> {refreshing ? t('Refreshing…') : t('Refresh episodes')}
              </button>
            )}
            {!viewer && <InfoTip text={t('Fetches the episode list again, so new episodes, new seasons and changed air dates show up.')} />}
            {admin && (
              <button className="btn-with-icon" onClick={() => void rescan()} disabled={rescanning} title={t("Looks at the files on your disk again: a file that was deleted by hand makes its episode wanted again, a file that was renamed is found, and a video file you put in the show's folder goes on its episode.")}>
                <Icon name="folder" size={16} /> {rescanning ? t('Checking…') : t('Rescan files')}
              </button>
            )}
            {series.downloadedCount > 0 && <WatchLinks tmdbId={series.tmdbId} kind="tv" />}
            {admin && (
              <button className="btn-with-icon danger-ghost" onClick={remove}>
                <Icon name="trash" size={16} /> {t('Remove')}
              </button>
            )}
          </>
        }
      >
        <div className="title-controls">
          <Switch checked={series.monitored} onChange={toggleSeries} disabled={!canManage} label={<span className="has-tip">{t('Monitored')}<InfoTip text={t('Mediarium keeps searching for the episodes of this show that are monitored and missing. Switch it off to leave the show alone; files you already have stay where they are.')} /></span>} description={[series.monitored ? t('Keeps looking for missing episodes.') : t('Not looking for missing episodes. Files you already have stay where they are.'), monitoring.detail].filter(Boolean).join(' ')} />
          <ProfilePicker kind="series" itemId={seriesId} onMoved={load}>
            <TitleTags kind="tv" id={seriesId} />
          </ProfilePicker>
          {canManage && <MonitorModePicker id={seriesId} onDone={load} />}
          {canManage && <SeriesTypePicker id={seriesId} value={series.seriesType ?? 'standard'} onChange={(type) => setSeries((cur) => (cur ? { ...cur, seriesType: type } : cur))} />}
        </div>
        {admin && series.folderPath && (
          <FolderLine path={series.folderPath} kind="series">
            <button type="button" className="btn-sm btn-with-icon" onClick={() => setChangingFolder(true)}>
              <Icon name="folder" size={14} /> {t('Change folder')}
            </button>
          </FolderLine>
        )}
        {changingFolder && <LocateDialog kind="series" id={seriesId} start={series.folderPath} onClose={() => setChangingFolder(false)} onDone={load} />}
        {linking && (
          <LocateDialog
            kind="episode"
            id={linking.id}
            label={linking.season === 0 ? t('Special {n}', { n: linking.episode }) : `S${String(linking.season).padStart(2, '0')}E${String(linking.episode).padStart(2, '0')}`}
            start={series.folderPath}
            onClose={() => setLinking(null)}
            onDone={load}
          />
        )}
        <WatchedLine kind="tv" id={seriesId} />
      </TitleHero>

      {error && <p className="error-text">{error}</p>}
      {message && <p className="badge downloaded">{message}</p>}

      {target && (
        <section ref={releasesRef} className="card" style={{ marginBottom: 24, scrollMarginTop: 80 }}>
          <h2>{t('Releases for {target}', { target: target.label })}</h2>
          <CustomSearch key={target.label} busy={searching} onSearch={(q) => runSearch(target, q)} />
          {searching && <p>{t('Looking for releases…')}</p>}
          {results !== null && <ReleaseNotice sources={answered.sources} unanswered={answered.unanswered} warnings={answered.warnings} searched={answered.searched} results={results.length} />}
          {!searching && (results === null || results.length === 0) && (
            <button className="btn-sm" onClick={() => setTarget(null)}>
              {t('Close')}
            </button>
          )}
          {blocked && (
            <GrabBlocked
              message={blocked.message}
              blocking={blocked.blocking}
              busy={grabbing !== null}
              onReplace={() => void grab(blocked.release, blocked.blocking.id, blocked.replace)}
              onDismiss={() => setBlocked(null)}
            />
          )}
          {results !== null && results.length > 0 && (
            <ReleaseTable results={results} searched={answered.searched} grabbing={grabbing} onGrab={(r) => void grab(r)} showPackBadge onClose={() => setTarget(null)} />
          )}
        </section>
      )}

      {canManage && <NumberingCard id={seriesId} seriesType={series.seriesType ?? 'standard'} seasons={seasons.map(([s]) => s).filter((s) => s > 0)} monitored={series.monitored} onRescanned={load} />}

      {seasons.length > 0 && (
        <div className="season-bar">
          <div className="chip-row" role="group" aria-label={t('Seasons')}>
            {seasons.map(([season, eps]) => (
              <button
                key={season}
                className={`chip${isOpen(season) ? ' active' : ''}`}
                onClick={() => {
                  setOpenSeasons((o) => ({ ...o, [season]: true }))
                  requestAnimationFrame(() => document.getElementById(`season-${season}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
                }}
              >
                {season === 0 ? t('Specials') : t('Season {n}', { n: season })}
                {eps.some(isMissing) && <span className="dot-missing" aria-label={t('has missing episodes')} />}
              </button>
            ))}
          </div>
          <div className="season-bar-actions">
            <label className="inline-field">
              <input type="checkbox" checked={missingOnly} onChange={(e) => setMissingOnly(e.target.checked)} />
              {t('Missing episodes only')}
              <InfoTip text={t('Shows only the episodes that have aired and are not downloaded yet.')} />
            </label>
            <label className="inline-field">
              {t('Seasons on this page')}
              <select value={seasonsMode} onChange={(e) => chooseSeasonsMode(e.target.value as SeasonsOpen)} aria-label={t('Seasons on this page')}>
                <option value="collapsed">{t('Collapsed')}</option>
                <option value="latest">{t('Latest season open')}</option>
                <option value="all">{t('All open')}</option>
              </select>
              <InfoTip text={t('Which seasons are open when you visit a show. Open all and Close all change the page right now. Your choice is kept in this browser.')} />
            </label>
            <button className="btn-sm" onClick={() => setOpenSeasons(Object.fromEntries(seasons.map(([s]) => [s, true])))}>
              {t('Open all')}
            </button>
            <button className="btn-sm" onClick={() => setOpenSeasons(Object.fromEntries(seasons.map(([s]) => [s, false])))}>
              {t('Close all')}
            </button>
          </div>
        </div>
      )}
      {missingOnly && shownSeasons.length === 0 && <p style={{ color: 'var(--text-dim)' }}>{t('No episodes that have aired are missing.')}</p>}

      {shownSeasons.map(([season, allEps]) => {
        const eps = missingOnly ? allEps.filter(isMissing) : allEps
        const done = allEps.filter((e) => e.status === 'downloaded').length
        const open = missingOnly || isOpen(season)
        return (
          <section key={season} id={`season-${season}`} style={{ marginBottom: open ? 28 : 12, scrollMarginTop: 80 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
              <button className="season-toggle" aria-expanded={open} onClick={() => setOpenSeasons((o) => ({ ...o, [season]: !open }))}>
                <Icon name={open ? 'chevron-down' : 'chevron-right'} size={18} />
                <h2 style={{ margin: 0 }}>{season === 0 ? t('Specials') : t('Season {n}', { n: season })}</h2>
              </button>
              <span className={`badge ${done === allEps.length ? 'downloaded' : 'missing'}`}>
                {done} / {allEps.length}
              </span>
              {allEps.some((e) => e.sizeBytes) && <span className="badge" title={t('Size on disk')}>{formatBytes(allEps.reduce((n, e) => n + (e.sizeBytes ?? 0), 0))}</span>}
              {season === 0 ? (
                <span className="hint">{t("Specials aren't looked for until you tick them. Each one is searched on its own, by S00E number and by its title.")}</span>
              ) : (
                <>
                  {canReleases && <button onClick={() => runSearch({ season, label: t('Season {n} (packs)', { n: season }) })}>{t('Search season')}</button>}
                  {canReleases && <InfoTip text={t('Lists the releases found for this whole season, season packs included, so you can pick one yourself.')} />}
                  {canManage && allEps.some(isMissing) && (
                    <button onClick={() => searchNow({ season })} disabled={searchingNow}>
                      {t('Find missing')}
                    </button>
                  )}
                  {canManage && allEps.some(isMissing) && <InfoTip text={t("Searches your indexers right now for this season's missing episodes and downloads what it finds. The rest of the show is left alone.")} />}
                  {admin && allEps.some((e) => e.status === 'downloaded' && e.filePath) && (
                    <button className="danger-ghost" onClick={() => void deleteFiles({ season })} title={t('Delete the files of this season and keep the show')}>
                      {t('Delete files')}
                    </button>
                  )}
                  <label style={{ display: 'flex', gap: 4, alignItems: 'center', fontSize: '0.85rem', color: 'var(--text-dim)' }}>
                    <input
                      type="checkbox"
                      checked={allEps.every((e) => e.monitored)}
                      disabled={!canManage}
                      onChange={(e) => toggleSeason(season, e.target.checked)}
                    />
                    {t('Monitored')}
                  </label>
                </>
              )}
            </div>
            {open && <table className="ep-table" style={{ marginTop: 8 }}>
              <thead>
                <tr>
                  <th style={{ width: 32 }}>
                    <span className="sr-only">{t('Monitored')}</span>
                  </th>
                  <th style={{ width: 48 }}>#</th>
                  <th>{t('Title')}</th>
                  <th>{t('Air date')}</th>
                  <th>{t('Status')}</th>
                  <th>{t('Quality')}</th>
                  <th>{t('Size')}</th>
                  <th>
                    <span className="sr-only">{t('Actions')}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {eps.map((e) => {
                  const unaired = !e.airDate || e.airDate > today
                  return (
                    <Fragment key={e.id}>
                    <tr>
                      <td>
                        <input
                          type="checkbox"
                          checked={e.monitored}
                          disabled={!canManage}
                          onChange={(ev) => toggleEpisode(e.id, ev.target.checked)}
                          aria-label={season === 0 ? t('Look for special {n}', { n: e.episode }) : t('Monitor episode {n}', { n: e.episode })}
                        />
                      </td>
                      <td>{e.episode}</td>
                      <td>
                        {e.title || <span style={{ color: 'var(--text-dim)' }}>{t('TBA')}</span>}
                        {e.status === 'downloaded' && <FileChips info={e.fileInfo} className="compact" />}
                        {e.status === 'downloaded' && <ShortFile compact note={e.shortFile} onRedo={admin ? () => void redoShortFile(e) : undefined} />}
                      </td>
                      <td style={{ color: 'var(--text-dim)' }}>{e.airDate || '—'}</td>
                      <td>
                        {e.status === 'missing' && unaired ? (
                          <span className="badge">{t('unaired')}</span>
                        ) : (
                          <span className={`badge ${e.status}`}>{EP_STATUS[e.status] ?? e.status}</span>
                        )}
                      </td>
                      <td>{qualityText(e.quality)}</td>
                      <td style={{ color: 'var(--text-dim)', whiteSpace: 'nowrap' }}>{e.sizeBytes ? formatBytes(e.sizeBytes) : '—'}</td>
                      <td>
                        {canReleases && (
                          <button
                            disabled={unaired && e.status === 'missing'}
                            onClick={() => runSearch({ season, episode: e.episode, label: season === 0 ? t('Special: {title}', { title: e.title || `S00E${String(e.episode).padStart(2, '0')}` }) : `S${String(season).padStart(2, '0')}E${String(e.episode).padStart(2, '0')}` })}
                          >
                            {t('Search')}
                          </button>
                        )}{' '}
                        {admin && e.status === 'downloaded' && e.filePath && (
                          <>
                            <button className="danger-ghost" onClick={() => void deleteFiles({ episode: e })} title={t('Delete this episode’s file and keep the show')}>
                              {t('Delete file')}
                            </button>{' '}
                          </>
                        )}
                        {admin && e.status !== 'downloading' && (
                          <>
                            <button className="btn-sm" onClick={() => setLinking(e)} title={t('Point this episode at a video file you choose')}>
                              {t('Link a file')}
                            </button>{' '}
                          </>
                        )}
                        {subtitlesOn && e.status === 'downloaded' && can(me, 'subtitles') && (
                          <button onClick={() => setSubsOpen(subsOpen === e.id ? null : e.id)}>{t('Subtitles')}</button>
                        )}
                      </td>
                    </tr>
                    {subtitlesOn && subsOpen === e.id && (
                      <tr>
                        <td colSpan={8}>
                          <SubtitlesPanel kind="episode" id={e.id} />
                        </td>
                      </tr>
                    )}
                    </Fragment>
                  )
                })}

              </tbody>
            </table>}
          </section>
        )
      })}

      <CastCard cast={tv?.cast ?? []} />

      {!series.copyOf && <TitleRequestActions kind="tv" tmdbId={series.tmdbId} libraryId={series.id} title={series.title} />}
      {!series.copyOf && <CopiesCard kind="series" id={series.id} admin={admin} />}

      <details className="card files-card" style={{ marginBottom: 24 }}>
        <summary>
          <h2 style={{ margin: 0, display: 'inline' }}>{t('What happened')}</h2>
        </summary>
        <div style={{ marginTop: 14 }}>
          <TitleEvents kind="series" id={series.id} />
        </div>
      </details>

      {series.downloadedCount > 0 && (
        <details className="card files-card" style={{ marginBottom: 24 }}>
          <summary>
            <h2 style={{ margin: 0, display: 'inline' }}>{t('Files on disk')}</h2>
          </summary>
          <div style={{ marginTop: 14 }}>
            <FilesPanel kind="series" id={series.id} />
          </div>
        </details>
      )}

      {series.downloadedCount > 0 && !subtitlesOn && <SubtitlesOffNote />}
    </div>
  )
}
