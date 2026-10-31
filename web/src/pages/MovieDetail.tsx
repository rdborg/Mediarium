import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import AddDialog from '../components/AddDialog'
import Icon from '../components/Icon'
import InfoTip from '../components/InfoTip'
import PosterCard from '../components/PosterCard'
import ProfilePicker from '../components/ProfilePicker'
import ReleaseTable from '../components/ReleaseTable'
import GrabBlocked from '../components/GrabBlocked'
import { blockingOf, type BlockingDownload } from '../grabBlocked'
import TitleTags from '../components/Tags'
import WatchedLine from '../components/WatchedLine'
import CollectionCard from '../components/CollectionCard'
import CopiesCard from '../components/CopiesCard'
import TitleRequestActions from '../components/TitleRequestActions'
import FilesPanel from '../components/FilesPanel'
import FileChips from '../components/FileChips'
import ShortFile from '../components/ShortFile'
import { redoAnswerText } from '../shortFile'
import SubtitlesOffNote from '../components/SubtitlesOffNote'
import SubtitlesPanel from '../components/SubtitlesPanel'
import Switch from '../components/Switch'
import TitleHero from '../components/TitleHero'
import { CastCard, PeopleLinks, titleLinks } from '../components/TitlePreview'
import { crewWith } from '../people'
import WatchLinks from '../components/WatchLinks'
import FolderLine from '../components/FolderLine'
import LocateDialog from '../components/LocateDialog'
import TitleEvents from '../components/TitleEvents'
import SearchStatus from '../components/SearchStatus'
import { describeState } from '../components/state'
import { useToast } from '../components/Toast'
import { useTorrentsSwitch } from '../useTorrentsSwitch'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { formatBytes, qualityText } from '../format'
import { rescanMessage } from '../rescanMessage'
import { movieFileToReplace, replaceQuestion } from '../replaceFile'
import { api, can, isAdmin, type DiscoverMovie, type MovieDetail as MovieDetailData, type SearchResult } from '../api'
import { useLive } from '../useLive'
import { useConfirm } from '../components/ConfirmProvider'
import { removeOption } from '../diskUsage'
import CustomSearch from '../components/CustomSearch'
import ReleaseNotice from '../components/ReleaseNotice'
import { releaseFacts } from '../releaseDates'
import type { Unanswered } from '../releaseHint'
import type { Searched } from '../searched'
import { useDocumentTitle } from '../documentTitle'
import { plural, t } from '../i18n'

// One page per movie. Not in your library: exactly one main action, "Add to
// library" (the dialog picks quality and where to download, and can start the
// search). In your library: "Find and download now" does the automatic search,
// and "Choose a release" is the manual version for when you want to pick.
export default function MovieDetail() {
  // Another movie (from "Similar movies") starts clean, so nothing of the last one stays on the page.
  return <MoviePage key={useParams<{ tmdbId: string }>().tmdbId} />
}

function MoviePage() {
  const confirm = useConfirm()
  const me = useAuth().user
  const admin = isAdmin(me)
  const { subtitlesOn } = useModules()
  const { tmdbId } = useParams<{ tmdbId: string }>()
  const navigate = useNavigate()
  const id = Number(tmdbId)
  const toast = useToast()
  const { grabError } = useTorrentsSwitch()

  const [movie, setMovie] = useState<MovieDetailData | null>(null)
  useDocumentTitle(movie?.title)
  const [similar, setSimilar] = useState<DiscoverMovie[]>([])
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const [locating, setLocating] = useState(false)
  const [monitored, setMonitored] = useState<boolean | null>(null)
  // What the movie's file takes on disk (0 when there is none).
  const [diskBytes, setDiskBytes] = useState(0)
  const [results, setResults] = useState<SearchResult[] | null>(null)
  // The releases list is drawn above the files and settings: bring it into view when a search answers.
  const releasesRef = useRef<HTMLElement>(null)
  const hasResults = results !== null
  useEffect(() => {
    if (hasResults) releasesRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [hasResults])
  const [answered, setAnswered] = useState<{ sources: number; unanswered: Unanswered[]; warnings?: string[]; searched?: Searched[]; ignoredSamples?: string[] }>({ sources: 0, unanswered: [] })
  const [searching, setSearching] = useState(false)
  const [searchingNow, setSearchingNow] = useState(false)
  const [grabbing, setGrabbing] = useState<string | null>(null)

  const [rescanning, setRescanning] = useState(false)
  async function rescan() {
    if (!movie?.libraryId) return
    setRescanning(true)
    try {
      const found = await api.rescanMovie(movie.libraryId)
      const text = rescanMessage(found, monitored !== false)
      if (found.looked || found.busy) toast.info(text)
      else toast.success(text)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setRescanning(false)
    }
  }

  async function deleteFile() {
    if (!movie?.libraryId) return
    const answer = await confirm({
      title: t('Delete the file of {title}?', { title: movie.title }),
      body: <p>{t('The file goes to the recycle bin (or is deleted, if it is switched off). The movie stays in your library and is wanted again, so Mediarium can look for a new release.')}</p>,
      confirmLabel: t('Delete'),
      danger: true,
    })
    if (!answer) return
    try {
      await api.deleteMovieFile(movie.libraryId)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  // A file far shorter than the movie should be: delete it and look for another release.
  async function redoShortFile() {
    if (!movie?.libraryId) return
    const answer = await confirm({
      title: t('Delete the file of {title} and search again?', { title: movie.title }),
      body: <p>{t('This file is much shorter than the movie should be, so it is most likely a fake or a cut-off download. It goes to the recycle bin (or is deleted, if that is switched off), the release it came from is blocklisted when Mediarium knows it, and Mediarium searches for another one.')}</p>,
      confirmLabel: t('Delete and search again'),
      danger: true,
    })
    if (!answer) return
    try {
      toast.info(redoAnswerText(await api.redoIncomplete({ movieIds: [movie.libraryId] })))
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  function reload() {
    api
      .tmdbMovieDetail(id)
      .then((m) => {
        if (m.tmdbId !== id) return // an answer for the movie we just left
        setMovie(m)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
    api.tmdbSimilarMovies(id).then(setSimilar).catch(() => undefined)
  }
  useEffect(reload, [id])
  // Only a title in the library changes by itself (a download starts, finishes); the details of one that isn't don't need re-reading.
  useLive(() => api.tmdbMovieDetail(id).then((m) => m.tmdbId === id && setMovie(m)).catch(() => undefined), 5000, !!movie?.libraryId)

  const libraryId = movie?.libraryId
  useEffect(() => {
    if (!libraryId) return
    api
      .getMovie(libraryId)
      .then((m) => {
        setMonitored(m.monitored ?? true)
        setDiskBytes(m.sizeBytes ?? 0)
      })
      .catch(() => undefined)
  }, [libraryId, movie?.filePath])

  async function toggleMonitored(v: boolean) {
    if (!libraryId) return
    try {
      await api.setMovieMonitored(libraryId, v)
      setMonitored(v)
      toast.success(v ? t('Monitored: looking for this movie.') : t('Unmonitored: this movie is left alone.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  async function searchNow() {
    if (!libraryId) return
    setSearchingNow(true)
    try {
      const res = await api.searchNowMovie(libraryId)
      toast.success(res.message)
      if (res.grabbed > 0) setTimeout(reload, 500)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSearchingNow(false)
    }
  }

  const [shrinking, setShrinking] = useState(false)
  async function replaceSmaller() {
    if (!libraryId || !movie?.filePath) return
    const size = (await api.getMovie(libraryId).catch(() => null))?.sizeBytes
    const file = movie.filePath.split(/[\\/]/).pop()
    const answer = await confirm({
      title: t('Replace {title} with a smaller version?', { title: movie.title }),
      body: (
        <>
          <p>{t('Mediarium looks for a release that this movie\'s quality profile accepts and that is smaller than the file you have now: {file}{size}.', { file: file ?? '', size: size ? ` (${formatBytes(size)})` : '' })}</p>
          <p>{t('Your current file stays until the new one is downloaded and in your library. If nothing suitable is found, or the download fails, nothing changes.')}</p>
        </>
      ),
      confirmLabel: t('Look for a smaller version'),
    })
    if (!answer) return
    setShrinking(true)
    try {
      const res = await api.replaceMovieSmaller(libraryId)
      toast.success(res.message)
      if (res.grabbed > 0) setTimeout(reload, 500)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setShrinking(false)
    }
  }

  // q is what was typed in the search box; the list stays on screen until its answer comes.
  async function chooseRelease(q?: string) {
    if (!libraryId) return
    setSearching(true)
    if (q === undefined) setResults(null)
    try {
      const a = await api.movieSearch(libraryId, q)
      setAnswered({ sources: a.sources, unanswered: a.unanswered, warnings: a.warnings, searched: a.searched, ignoredSamples: a.ignoredSamples })
      setResults(a.results)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSearching(false)
    }
  }

  // A grab that was refused because another download is in the way, with the
  // release that was picked, so it can be repeated once that one is cancelled.
  const [blocked, setBlocked] = useState<{ release: SearchResult; blocking: BlockingDownload; message: string; replace: boolean } | null>(null)

  // replaceFile is what the person already decided about the file the movie
  // has (when the grab is repeated); left out, a movie that has a file asks first.
  async function grab(r: SearchResult, replaceQueueId?: number, replaceFile?: boolean) {
    if (!libraryId) return
    let replace = replaceFile ?? false
    if (replaceFile === undefined) {
      const old = movie ? movieFileToReplace({ status: movie.status ?? '', filePath: movie.filePath, quality: movie.quality, sizeBytes: diskBytes }) : null
      if (old) {
        const q = replaceQuestion([old], r)
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
    setBlocked(null)
    try {
      await api.grab(libraryId, { releaseTitle: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol, replaceQueueId, replace: replace || undefined })
      toast.success(t('Started downloading "{title}". Follow it in Activity.', { title: r.title }))
      setTimeout(reload, 500)
    } catch (e) {
      const blocking = blockingOf(e)
      if (blocking) setBlocked({ release: r, blocking, message: e instanceof Error ? e.message : String(e), replace })
      else grabError(e, () => void grab(r, replaceQueueId, replace))
    } finally {
      setGrabbing(null)
    }
  }

  async function onRemove() {
    if (!movie?.libraryId) return
    const usage = await api.movieDiskUsage(movie.libraryId).catch(() => null)
    const copies = (await api.titleCopies('movie', movie.libraryId).catch(() => null))?.items ?? []
    const answer = await confirm({
      title: t('Remove {title} from your library?', { title: movie.title }),
      body: (
        <p>
          {t('Mediarium stops tracking this movie and cancels anything still downloading for it. You can add it back any time.')}
          {copies.length > 0 && ' ' + plural(copies.length, 'Its second copy ({names}) goes too, with its file if you tick the box.', 'Its second copies ({names}) go too, with their files if you tick the box.', { names: copies.map((c) => c.profileName).join(', ') })}
        </p>
      ),
      confirmLabel: t('Remove'),
      danger: true,
      option: removeOption('movie', usage),
    })
    if (!answer) return
    const deleteFiles = answer.checked
    try {
      await api.deleteMovie(movie.libraryId, deleteFiles)
      toast.success(t('{title} removed.', { title: movie.title }))
      navigate('/library')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (error && !movie) return <p className="error-text">{error}</p>
  if (!movie) return <div className="skeleton" style={{ height: 380, borderRadius: 22 }} />

  const inLibrary = !!movie.libraryId
  const directors = crewWith(movie.crew, 'Director')
  const writers = crewWith(movie.crew, 'Writer')
  const stateKey = !inLibrary ? null : movie.status === 'downloaded' ? 'downloaded' : movie.status === 'downloading' ? 'downloading' : monitored === false ? 'added' : 'searching'
  const state = stateKey ? describeState(stateKey) : null

  return (
    <div>
      <TitleHero
        info={{
          ...movie,
          kind: 'movie',
          facts: [
            ...(directors.length ? [{ label: directors.length > 1 ? t('Directors') : t('Director'), value: <PeopleLinks people={directors} /> }] : []),
            ...(writers.length ? [{ label: writers.length > 1 ? t('Writers') : t('Writer'), value: <PeopleLinks people={writers} /> }] : []),
            ...releaseFacts(movie.releaseDates, movie.releaseDate),
            ...(movie.releaseStatus ? [{ label: t('Status'), value: movie.releaseStatus }] : []),
            ...(inLibrary && movie.filePath && diskBytes > 0 ? [{ label: t('Size on disk'), value: formatBytes(diskBytes) }] : []),
          ],
          links: titleLinks('movie', movie.tmdbId, movie.imdbId, movie.homepage),
        }}
        status={
          state && (
            <span className={`state-tag st-${state.key}`} title={state.hint}>
              <Icon name={state.icon} size={13} /> {state.label}
              {movie.quality ? ` · ${qualityText(movie.quality)}` : ''}
            </span>
          )
        }
        actions={
          inLibrary ? (
            <>
              {can(me, 'manage') && (
                <button className="primary btn-with-icon" onClick={searchNow} disabled={searchingNow || movie.status === 'downloading'}>
                  <Icon name="search" size={16} /> {searchingNow ? t('Searching…') : movie.status === 'downloaded' ? t('Look for a better copy') : t('Find and download now')}
                </button>
              )}
              {can(me, 'manage') && (
                <InfoTip
                  text={
                    movie.status === 'downloaded'
                      ? t('Searches your indexers right now for a better release than the file you have. It only looks when "Better versions" is on and the file is below the profile\'s cutoff.')
                      : t('Searches your indexers right now and starts the best release for the quality profile, even if the movie is not monitored.')
                  }
                />
              )}
              {can(me, 'manage') && movie.status === 'downloaded' && movie.filePath && (
                <button className="btn-with-icon" onClick={() => void replaceSmaller()} disabled={shrinking}>
                  <Icon name="download" size={16} /> {shrinking ? t('Searching…') : t('Replace with a smaller version')}
                </button>
              )}
              {can(me, 'manage') && movie.status === 'downloaded' && movie.filePath && (
                <InfoTip text={t('For a movie you moved to a lower quality profile, 4K to 1080p say. Looks for a smaller release the profile accepts, downloads it, and only then removes the big file.')} />
              )}
              {can(me, 'releases') && (
                <button className="btn-with-icon" onClick={() => chooseRelease()} disabled={searching}>
                  <Icon name="list" size={16} /> {searching ? t('Loading…') : t('Choose a release')}
                </button>
              )}
              {can(me, 'releases') && <InfoTip text={t('Lists every release your indexers found for this movie, so you can pick one yourself instead of letting Mediarium choose.')} />}
              {movie.status === 'downloaded' && <WatchLinks tmdbId={movie.tmdbId} kind="movie" />}
              {admin && ((movie.status === 'downloaded' && movie.filePath) || movie.status === 'missing') && (
                <button className="btn-with-icon" onClick={() => void rescan()} disabled={rescanning} title={movie.filePath ? t('Looks at the file on your disk again: if it was deleted by hand the movie is wanted again, and a renamed file is found.') : t("Looks in the movie's folder for a video file you put there by hand and adds it to the movie.")}>
                  <Icon name="folder" size={16} /> {rescanning ? t('Checking…') : t('Rescan files')}
                </button>
              )}
              {admin && movie.status === 'downloaded' && movie.filePath && (
                <button className="btn-with-icon danger-ghost" onClick={() => void deleteFile()} title={t('Delete the file and keep the movie')}>
                  <Icon name="trash" size={16} /> {t('Delete file')}
                </button>
              )}
              {admin && (
                <button className="btn-with-icon danger-ghost" onClick={onRemove}>
                  <Icon name="trash" size={16} /> {t('Remove')}
                </button>
              )}
            </>
          ) : (
            <button className="primary btn-with-icon big" onClick={() => setAdding(true)}>
              <Icon name="plus" size={18} /> {t('Add to library')}
            </button>
          )
        }
      >
        {inLibrary && movie.status === 'missing' && movie.libraryId && <SearchStatus kind="movie" id={movie.libraryId} />}
        {inLibrary && (
          <div className="title-controls">
            <Switch checked={monitored ?? true} onChange={toggleMonitored} disabled={!can(me, 'manage')} label={<span className="has-tip">{t('Monitored')}<InfoTip text={t('Mediarium keeps searching and downloads a release when one turns up. Switch it off to leave this movie alone; files you already have stay where they are.')} /></span>} description={monitored ?? true ? t('Keeps looking until it finds this movie.') : t('Not looking for this movie. Files you already have stay where they are.')} />
            <ProfilePicker kind="movie" itemId={movie.libraryId!} onMoved={reload}>
              {movie.libraryId && <TitleTags kind="movie" id={movie.libraryId} />}
            </ProfilePicker>
            {movie.filePath && <code className="filepath">{movie.filePath}</code>}
          </div>
        )}
        {inLibrary && movie.status === 'downloaded' && <FileChips info={movie.fileInfo} />}
        {inLibrary && movie.status === 'downloaded' && <ShortFile note={movie.shortFile} onRedo={admin ? () => void redoShortFile() : undefined} />}
        {inLibrary && admin && movie.folderPath && (
          <FolderLine path={movie.folderPath} kind="movie">
            {movie.libraryId && (
              <button type="button" className="btn-sm btn-with-icon" onClick={() => setLocating(true)}>
                <Icon name="folder" size={14} /> {t('Locate the file')}
              </button>
            )}
          </FolderLine>
        )}
        {locating && movie.libraryId && <LocateDialog kind="movie" id={movie.libraryId} start={movie.folderPath} onClose={() => setLocating(false)} onDone={reload} />}
        {inLibrary && movie.libraryId && <WatchedLine kind="movie" id={movie.libraryId} />}
      </TitleHero>

      <CastCard cast={movie.cast ?? []} />

      {adding && (
        <AddDialog
          target={{ kind: 'movie', tmdbId: movie.tmdbId, title: movie.title, year: movie.year, posterUrl: movie.posterUrl, overview: movie.overview }}
          onClose={() => setAdding(false)}
          onAdded={() => {
            setAdding(false)
            reload()
          }}
        />
      )}

      {results !== null && (
        <section ref={releasesRef} className="card" style={{ marginBottom: 24, scrollMarginTop: 80 }}>
          <h2>{t('Releases')}</h2>
          <CustomSearch busy={searching} onSearch={(q) => chooseRelease(q)} />
          <ReleaseNotice sources={answered.sources} unanswered={answered.unanswered} warnings={answered.warnings} searched={answered.searched} ignoredSamples={answered.ignoredSamples} results={results.length} />
          {results.length === 0 && (
            <button className="btn-sm" onClick={() => setResults(null)}>
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
          {results.length > 0 && <ReleaseTable results={results} searched={answered.searched} grabbing={grabbing} onGrab={(r) => void grab(r)} onClose={() => setResults(null)} />}
        </section>
      )}

      {movie.libraryId && <TitleRequestActions kind="movie" tmdbId={movie.tmdbId} libraryId={movie.libraryId} title={movie.title} />}
      {movie.libraryId && <CopiesCard kind="movie" id={movie.libraryId} admin={admin} />}

      {movie.libraryId && (
        <section className="card" style={{ marginBottom: 24 }}>
          <h2>{t('What happened')}</h2>
          <TitleEvents kind="movie" id={movie.libraryId} />
        </section>
      )}

      {movie.libraryId && movie.status === 'downloaded' && (
        <>
          <div className={subtitlesOn ? 'half-cols' : undefined} style={{ marginBottom: subtitlesOn ? 24 : 12 }}>
            <section className="card">
              <h2>{t('Files')}</h2>
              <FilesPanel kind="movie" id={movie.libraryId} />
            </section>
            {subtitlesOn && (
              <section className="card">
                <h2>{t('Subtitles')}</h2>
                <SubtitlesPanel kind="movie" id={movie.libraryId} />
              </section>
            )}
          </div>
          {!subtitlesOn && <SubtitlesOffNote />}
        </>
      )}

      {movie.collection && <CollectionCard collection={movie.collection} admin={admin} onChanged={reload} />}

      {similar.length > 0 && (
        <section>
          <div className="rail-head">
            <h2>{t('Similar movies')}</h2>
          </div>
          <div className="poster-grid">
            {similar.map((m) => (
              <PosterCard key={m.tmdbId} to={`/title/${m.tmdbId}`} poster={m.posterUrl} title={m.title} meta={m.year || undefined} genres={m.genres} rating={m.rating} kind="movie" />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
