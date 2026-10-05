import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import AddDialog from '../components/AddDialog'
import Icon from '../components/Icon'
import PosterCard from '../components/PosterCard'
import ProfilePicker from '../components/ProfilePicker'
import ReleaseTable from '../components/ReleaseTable'
import TitleTags from '../components/Tags'
import FilesPanel from '../components/FilesPanel'
import SubtitlesOffNote from '../components/SubtitlesOffNote'
import SubtitlesPanel from '../components/SubtitlesPanel'
import Switch from '../components/Switch'
import TitleHero from '../components/TitleHero'
import { CastCard, titleLinks } from '../components/TitlePreview'
import WatchLinks from '../components/WatchLinks'
import TitleEvents from '../components/TitleEvents'
import SearchStatus from '../components/SearchStatus'
import { describeState } from '../components/state'
import { useToast } from '../components/Toast'
import { useAuth } from '../AuthContext'
import { useModules } from '../ModulesContext'
import { qualityText } from '../format'
import { api, isAdmin, type DiscoverMovie, type MovieDetail as MovieDetailData, type SearchResult } from '../api'
import { useLive } from '../useLive'
import { useConfirm } from '../components/ConfirmProvider'
import { removeOption } from '../diskUsage'
import ReleaseNotice from '../components/ReleaseNotice'
import type { Unanswered } from '../releaseHint'
import { useDocumentTitle } from '../documentTitle'

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
  const admin = isAdmin(useAuth().user)
  const { subtitlesOn } = useModules()
  const { tmdbId } = useParams<{ tmdbId: string }>()
  const navigate = useNavigate()
  const id = Number(tmdbId)
  const toast = useToast()

  const [movie, setMovie] = useState<MovieDetailData | null>(null)
  useDocumentTitle(movie?.title)
  const [similar, setSimilar] = useState<DiscoverMovie[]>([])
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const [monitored, setMonitored] = useState<boolean | null>(null)
  const [results, setResults] = useState<SearchResult[] | null>(null)
  const [answered, setAnswered] = useState<{ sources: number; unanswered: Unanswered[] }>({ sources: 0, unanswered: [] })
  const [searching, setSearching] = useState(false)
  const [searchingNow, setSearchingNow] = useState(false)
  const [grabbing, setGrabbing] = useState<string | null>(null)

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
  useLive(() => api.tmdbMovieDetail(id).then((m) => m.tmdbId === id && setMovie(m)).catch(() => undefined), 5000)

  const libraryId = movie?.libraryId
  useEffect(() => {
    if (!libraryId) return
    api
      .getMovie(libraryId)
      .then((m) => setMonitored(m.monitored ?? true))
      .catch(() => undefined)
  }, [libraryId])

  async function toggleMonitored(v: boolean) {
    if (!libraryId) return
    try {
      await api.setMovieMonitored(libraryId, v)
      setMonitored(v)
      toast.success(v ? 'Monitored: looking for this movie.' : 'Unmonitored: this movie is left alone.')
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

  async function chooseRelease() {
    if (!libraryId) return
    setSearching(true)
    setResults(null)
    try {
      const a = await api.movieSearch(libraryId)
      setAnswered({ sources: a.sources, unanswered: a.unanswered })
      setResults(a.results)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSearching(false)
    }
  }

  async function grab(r: SearchResult) {
    if (!libraryId) return
    setGrabbing(r.downloadUrl)
    try {
      await api.grab(libraryId, { releaseTitle: r.title, downloadUrl: r.downloadUrl, sizeBytes: r.sizeBytes, protocol: r.protocol })
      toast.success(`Started downloading "${r.title}". Follow it in Activity.`)
      setTimeout(reload, 500)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setGrabbing(null)
    }
  }

  async function onRemove() {
    if (!movie?.libraryId) return
    const usage = await api.movieDiskUsage(movie.libraryId).catch(() => null)
    const answer = await confirm({
      title: `Remove ${movie.title} from your library?`,
      body: <p>Mediarium stops tracking this movie and cancels anything still downloading for it. You can add it back any time.</p>,
      confirmLabel: 'Remove',
      danger: true,
      option: removeOption('movie', usage),
    })
    if (!answer) return
    const deleteFiles = answer.checked
    try {
      await api.deleteMovie(movie.libraryId, deleteFiles)
      toast.success(`${movie.title} removed.`)
      navigate('/library')
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  if (error && !movie) return <p className="error-text">{error}</p>
  if (!movie) return <div className="skeleton" style={{ height: 380, borderRadius: 22 }} />

  const inLibrary = !!movie.libraryId
  const stateKey = !inLibrary ? null : movie.status === 'downloaded' ? 'downloaded' : movie.status === 'downloading' ? 'downloading' : monitored === false ? 'added' : 'searching'
  const state = stateKey ? describeState(stateKey) : null

  return (
    <div>
      <TitleHero
        info={{
          ...movie,
          kind: 'movie',
          facts: [
            ...(movie.directors?.length ? [{ label: movie.directors.length > 1 ? 'Directors' : 'Director', value: movie.directors.join(', ') }] : []),
            ...(movie.releaseDate ? [{ label: 'Released', value: movie.releaseDate }] : []),
            ...(movie.releaseStatus ? [{ label: 'Status', value: movie.releaseStatus }] : []),
          ],
          links: inLibrary ? undefined : titleLinks('movie', movie.tmdbId, movie.imdbId, movie.homepage),
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
              <button className="primary btn-with-icon" onClick={searchNow} disabled={searchingNow || movie.status === 'downloading'}>
                <Icon name="search" size={16} /> {searchingNow ? 'Searching…' : movie.status === 'downloaded' ? 'Look for a better copy' : 'Find and download now'}
              </button>
              <button className="btn-with-icon" onClick={chooseRelease} disabled={searching}>
                <Icon name="list" size={16} /> {searching ? 'Loading…' : 'Choose a release'}
              </button>
              {movie.status === 'downloaded' && <WatchLinks tmdbId={movie.tmdbId} kind="movie" />}
              {admin && (
                <button className="btn-with-icon danger-ghost" onClick={onRemove}>
                  <Icon name="trash" size={16} /> Remove
                </button>
              )}
            </>
          ) : (
            <button className="primary btn-with-icon big" onClick={() => setAdding(true)}>
              <Icon name="plus" size={18} /> Add to library
            </button>
          )
        }
      >
        {inLibrary && movie.status === 'missing' && movie.libraryId && <SearchStatus kind="movie" id={movie.libraryId} />}
        {inLibrary && (
          <div className="title-controls">
            <Switch checked={monitored ?? true} onChange={toggleMonitored} label="Monitored" description="Keeps looking until it finds this movie." />
            <ProfilePicker kind="movie" itemId={movie.libraryId!} />
            {movie.filePath && <code className="filepath">{movie.filePath}</code>}
          </div>
        )}
        {inLibrary && movie.libraryId && <TitleTags kind="movie" id={movie.libraryId} />}
      </TitleHero>

      {!inLibrary && <CastCard cast={movie.cast ?? []} />}

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
        <section className="card" style={{ marginBottom: 24 }}>
          <h2>Releases</h2>
          <ReleaseNotice sources={answered.sources} unanswered={answered.unanswered} results={results.length} />
          {results.length > 0 && <ReleaseTable results={results} grabbing={grabbing} onGrab={grab} />}
        </section>
      )}

      {movie.libraryId && (
        <section className="card" style={{ marginBottom: 24 }}>
          <h2>What happened</h2>
          <TitleEvents kind="movie" id={movie.libraryId} />
        </section>
      )}

      {movie.libraryId && movie.status === 'downloaded' && (
        <>
          <div className={subtitlesOn ? 'half-cols' : undefined} style={{ marginBottom: subtitlesOn ? 24 : 12 }}>
            <section className="card">
              <h2>Files</h2>
              <FilesPanel kind="movie" id={movie.libraryId} />
            </section>
            {subtitlesOn && (
              <section className="card">
                <h2>Subtitles</h2>
                <SubtitlesPanel kind="movie" id={movie.libraryId} />
              </section>
            )}
          </div>
          {!subtitlesOn && <SubtitlesOffNote />}
        </>
      )}

      {similar.length > 0 && (
        <section>
          <div className="rail-head">
            <h2>Similar movies</h2>
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
