import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import AddDialog from '../components/AddDialog'
import Icon from '../components/Icon'
import TitleHero from '../components/TitleHero'
import { CastCard, SeasonsCard, titleLinks } from '../components/TitlePreview'
import { api, type TVDetail } from '../api'
import { useDocumentTitle } from '../documentTitle'

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

  const facts = [
    show.creators?.length ? { label: show.creators.length > 1 ? 'Creators' : 'Creator', value: show.creators.join(', ') } : null,
    show.firstAirDate ? { label: 'First aired', value: show.firstAirDate } : null,
    show.seasons ? { label: 'Seasons', value: String(show.seasons) } : null,
    show.episodes ? { label: 'Episodes', value: String(show.episodes) } : null,
    show.networks?.length ? { label: 'Network', value: show.networks.join(', ') } : null,
    show.releaseStatus ? { label: 'Status', value: show.releaseStatus } : null,
  ].filter(Boolean) as { label: string; value: string }[]

  return (
    <div>
      <TitleHero
        info={{ ...show, kind: 'tv', certification: show.contentRating ?? show.certification, facts, links: titleLinks('tv', show.tmdbId, show.imdbId, show.homepage) }}
        actions={
          <button className="primary btn-with-icon big" onClick={() => setAdding(true)}>
            <Icon name="plus" size={18} /> Add to library
          </button>
        }
      />
      <SeasonsCard seasons={show.seasonList ?? []} />
      <CastCard cast={show.cast ?? []} />
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
