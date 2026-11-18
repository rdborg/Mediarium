import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, type PersonCredit, type PersonPage } from '../api'
import LoadFailed from '../components/LoadFailed'
import AddDialog, { type AddTarget } from '../components/AddDialog'
import { DiscoverCards } from '../components/DiscoverRail'
import Icon from '../components/Icon'
import { useDocumentTitle } from '../documentTitle'
import { t } from '../i18n'
import { ageOf, bioIsLong, CREDITS_FIRST, filterByRole, firstOf, longDate, roleFilterFrom, showRoleFilter, type RoleFilter } from '../people'
import { useOwned } from '../useOwned'
import { useUrlState } from '../useUrlState'
import './Person.css'

const FILTERS: { value: RoleFilter; label: () => string }[] = [
  { value: 'all', label: () => t('All') },
  { value: 'acting', label: () => t('Acting') },
  { value: 'directing', label: () => t('Directing') },
  { value: 'writing', label: () => t('Writing') },
]

// A person's page: who they are, then the movies and shows they worked on,
// the most popular first, as the same cards Discover uses.
export default function Person() {
  const { id } = useParams<{ id: string }>()
  const personId = Number(id)
  const owned = useOwned()
  const [person, setPerson] = useState<PersonPage | null>(null)
  const [error, setError] = useState('')
  const [adding, setAdding] = useState<AddTarget | null>(null)
  const [bioOpen, setBioOpen] = useState(false)
  const [photoBroken, setPhotoBroken] = useState(false)
  const [more, setMore] = useState({ movies: CREDITS_FIRST, shows: CREDITS_FIRST })
  const [ui, patchUi] = useUrlState({ role: 'all' })
  useDocumentTitle(person?.name)

  useEffect(() => {
    setPerson(null)
    setError('')
    setBioOpen(false)
    setPhotoBroken(false)
    setMore({ movies: CREDITS_FIRST, shows: CREDITS_FIRST })
    // Going from one person to another: the answer for the first must not land on the second.
    let current = true
    api
      .tmdbPerson(personId)
      .then((p) => current && setPerson(p))
      .catch((e) => current && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      current = false
    }
  }, [personId])

  const role = roleFilterFrom(ui.role, person?.workKinds ?? [])
  const movies = useMemo(() => filterByRole(person?.movies ?? [], role), [person, role])
  const shows = useMemo(() => filterByRole(person?.shows ?? [], role), [person, role])

  if (error) return <LoadFailed message={error} />
  if (!person) return <div className="skeleton" style={{ height: 320, borderRadius: 22 }} />

  const age = ageOf(person.birthday, person.deathday)
  const born = person.birthday ? (age !== undefined && !person.deathday ? t('{date} (age {age})', { date: longDate(person.birthday), age }) : longDate(person.birthday)) : ''
  const died = person.deathday ? (age !== undefined ? t('{date} (age {age})', { date: longDate(person.deathday), age }) : longDate(person.deathday)) : ''
  const facts = [
    born ? { label: t('Born'), value: born } : null,
    died ? { label: t('Died'), value: died } : null,
    person.placeOfBirth ? { label: t('Place of birth'), value: person.placeOfBirth } : null,
  ].filter(Boolean) as { label: string; value: string }[]
  const long = bioIsLong(person.biography)
  const nothing = person.movies.length === 0 && person.shows.length === 0

  return (
    <div className="person-page">
      <section className="person-head">
        <div className="person-photo">
          {person.photoUrl && !photoBroken ? <img src={person.photoUrl} alt="" onError={() => setPhotoBroken(true)} /> : <span aria-hidden="true">{person.name.charAt(0).toUpperCase()}</span>}
        </div>
        <div className="person-info">
          <h1>{person.name}</h1>
          {facts.length > 0 && (
            <dl className="title-facts">
              {facts.map((f) => (
                <div key={f.label}>
                  <dt>{f.label}</dt>
                  <dd>{f.value}</dd>
                </div>
              ))}
            </dl>
          )}
          {person.biography && (
            <div>
              <p className={`person-bio${long && !bioOpen ? ' folded' : ''}`}>{person.biography}</p>
              {long && (
                <button type="button" className="btn-sm" aria-expanded={bioOpen} onClick={() => setBioOpen(!bioOpen)}>
                  {bioOpen ? t('Show less') : t('Show more')}
                </button>
              )}
            </div>
          )}
          <div className="title-links">
            {person.imdbId && /^nm\d+$/.test(person.imdbId) && (
              <a className="link-pill" href={`https://www.imdb.com/name/${person.imdbId}/`} target="_blank" rel="noreferrer">
                <Icon name="external" size={13} /> IMDb
              </a>
            )}
            <a className="link-pill" href={`https://www.themoviedb.org/person/${person.id}`} target="_blank" rel="noreferrer">
              <Icon name="external" size={13} /> TMDB
            </a>
          </div>
        </div>
      </section>

      {showRoleFilter(person.workKinds) && (
        <div className="seg person-filter" role="group" aria-label={t('Kind of work')}>
          {FILTERS.filter((f) => f.value === 'all' || person.workKinds.includes(f.value)).map((f) => (
            <button key={f.value} className={role === f.value ? 'active' : ''} aria-pressed={role === f.value} onClick={() => { patchUi({ role: f.value }); setMore({ movies: CREDITS_FIRST, shows: CREDITS_FIRST }) }}>
              {f.label()}
            </button>
          ))}
        </div>
      )}

      {nothing && <p className="person-empty">{t('TMDB lists no movies or shows for {name}.', { name: person.name })}</p>}
      <Credits title={t('Movies')} kind="movie" items={movies} shown={more.movies} onMore={() => setMore((m) => ({ ...m, movies: m.movies + CREDITS_FIRST }))} owned={owned} onAdd={setAdding} />
      <Credits title={t('TV shows')} kind="tv" items={shows} shown={more.shows} onMore={() => setMore((m) => ({ ...m, shows: m.shows + CREDITS_FIRST }))} owned={owned} onAdd={setAdding} />

      {adding && (
        <AddDialog
          target={adding}
          onClose={() => setAdding(null)}
          onAdded={() => {
            // Stay here so you can keep browsing: the card switches to "Open" by itself.
            setAdding(null)
            owned.reload()
          }}
        />
      )}
    </div>
  )
}

function Credits({ title, kind, items, shown, onMore, owned, onAdd }: { title: string; kind: 'movie' | 'tv'; items: PersonCredit[]; shown: number; onMore: () => void; owned: ReturnType<typeof useOwned>; onAdd: (t: AddTarget) => void }) {
  if (items.length === 0) return null
  const { visible, hidden } = firstOf(items, shown)
  return (
    <section className="discover-rail person-credits">
      <div className="rail-head">
        <h2>{title}</h2>
      </div>
      <div className="poster-grid">
        <DiscoverCards items={visible} kind={kind} owned={owned} onAdd={onAdd} />
      </div>
      {hidden > 0 && (
        <button type="button" className="btn-sm person-more" onClick={onMore}>
          {t('Show more')}
        </button>
      )}
    </section>
  )
}
