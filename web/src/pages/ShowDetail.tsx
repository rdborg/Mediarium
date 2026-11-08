import { useEffect, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import AddDialog from '../components/AddDialog'
import Icon from '../components/Icon'
import TitleHero from '../components/TitleHero'
import { CastCard, PeopleLinks, SeasonsCard, titleLinks } from '../components/TitlePreview'
import { crewWith } from '../people'
import { api, isViewer, type RequestStatus, type TVDetail } from '../api'
import { useAuth } from '../AuthContext'
import { AskEpisodeDialog } from '../components/RequestDialogs'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'

// A TV show that is not in the library yet: full info plus one action.
// Shows you already own go straight to their library page instead.
export default function ShowDetail() {
  const { tmdbId } = useParams<{ tmdbId: string }>()
  const navigate = useNavigate()
  const id = Number(tmdbId)
  const [show, setShow] = useState<TVDetail | null>(null)
  useDocumentTitle(show?.title)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState(false)
  const [askingEpisode, setAskingEpisode] = useState(false)
  // An account that has to ask can ask for one episode instead of the whole show.
  const viewer = isViewer(useAuth().user)
  const [status, setStatus] = useState<RequestStatus | null>(null)
  useEffect(() => {
    if (viewer) return
    api.requestStatus().then(setStatus).catch(() => setStatus(null))
  }, [viewer])

  useEffect(() => {
    api
      .tmdbTVDetail(id)
      .then((d) => {
        if (d.libraryId) navigate(`/series/${d.libraryId}`, { replace: true })
        else setShow(d)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, navigate])

  if (error) return <p className="error-text">{error}</p>
  if (!show) return <div className="skeleton" style={{ height: 380, borderRadius: 22 }} />

  const creators = crewWith(show.crew, 'Creator')
  const facts = [
    creators.length ? { label: creators.length > 1 ? t('Creators') : t('Creator'), value: <PeopleLinks people={creators} /> } : null,
    show.firstAirDate ? { label: t('First aired'), value: show.firstAirDate } : null,
    show.seasons ? { label: t('Seasons'), value: String(show.seasons) } : null,
    show.episodes ? { label: t('Episodes'), value: String(show.episodes) } : null,
    show.networks?.length ? { label: t('Network'), value: show.networks.join(', ') } : null,
    show.releaseStatus ? { label: t('Status'), value: show.releaseStatus } : null,
  ].filter(Boolean) as { label: string; value: ReactNode }[]

  return (
    <div>
      <TitleHero
        info={{ ...show, kind: 'tv', certification: show.contentRating ?? show.certification, facts, links: titleLinks('tv', show.tmdbId, show.imdbId, show.homepage), tmdbId: show.tmdbId, imdbId: show.imdbId }}
        actions={
          <>
            <button className="primary btn-with-icon big" onClick={() => setAdding(true)}>
              <Icon name="plus" size={18} /> {t('Add to library')}
            </button>
            {status?.asksToAdd && (show.seasonList ?? []).length > 0 && (
              <button className="btn-with-icon big" onClick={() => setAskingEpisode(true)}>
                <Icon name="tv" size={18} /> {t('Ask for one episode')}
              </button>
            )}
          </>
        }
      />
      <SeasonsCard seasons={show.seasonList ?? []} />
      <CastCard cast={show.cast ?? []} />
      {askingEpisode && <AskEpisodeDialog tmdbId={show.tmdbId} title={show.title} seasons={show.seasonList ?? []} onClose={() => setAskingEpisode(false)} />}
      {adding && (
        <AddDialog
          target={{ kind: 'tv', tmdbId: show.tmdbId, title: show.title, year: show.year, posterUrl: show.posterUrl, overview: show.overview }}
          onClose={() => setAdding(false)}
          onAdded={(libraryId) => navigate(`/series/${libraryId}`)}
        />
      )}
    </div>
  )
}
