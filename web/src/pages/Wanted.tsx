import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, isAdmin, type Movie, type MusicWanted, type Series, type SubtitleQuota, type SubtitleWanted, type WantedItem } from '../api'
import { useAuth } from '../AuthContext'
import Cover from '../components/Cover'
import Icon, { type IconName } from '../components/Icon'
import Loading from '../components/Loading'
import { PosterFallback } from '../components/PosterCard'
import { KIND_LABEL, useKinds, useModules, type MediaKind } from '../ModulesContext'
import QuotaNote from '../components/QuotaNote'
import { describeState } from '../components/state'
import { useToast } from '../components/Toast'
import { languageName } from '../languages'
import { useLive } from '../useLive'

type Kind = 'missing' | 'cutoff' | 'subtitles'

const KIND_ICON: Record<MediaKind, IconName> = { movie: 'film', tv: 'tv', music: 'music' }

// Wanted: what Mediarium is still hunting for. "Missing" is monitored movies
// with nothing downloaded and aired episodes without a file; "Upgrades" is
// downloaded items still below their profile's cutoff; "Subtitles" is
// downloaded titles missing a language you asked for.
export default function Wanted() {
  const toast = useToast()
  // Marking subtitles as not needed is for administrators.
  const admin = isAdmin(useAuth().user)
  const [params] = useSearchParams()
  const [chosenKind, setKind] = useState<Kind>(params.get('tab') === 'subtitles' ? 'subtitles' : 'missing')
  // The Subtitles tab, its count and its list exist only while subtitles are switched on.
  const { subtitlesOn } = useModules()
  const kind: Kind = chosenKind === 'subtitles' && !subtitlesOn ? 'missing' : chosenKind
  const [quota, setQuota] = useState<SubtitleQuota | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [showDismissed, setShowDismissed] = useState(false)
  const [items, setItems] = useState<WantedItem[] | null>(null)
  const [subs, setSubs] = useState<SubtitleWanted[] | null>(null)
  const [movies, setMovies] = useState<Movie[]>([])
  const [series, setSeries] = useState<Series[]>([])
  const [sweeping, setSweeping] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [counts, setCounts] = useState<{ missing?: number; cutoff?: number; subtitles?: number }>({})
  // Music albums, when the music module is on, share the same lists.
  const kinds = useKinds()
  const musicOn = kinds.includes('music')
  const [media, setMedia] = useState<'all' | MediaKind>('all')
  const [music, setMusic] = useState<MusicWanted[] | null>(null)
  const [musicCounts, setMusicCounts] = useState<{ missing?: number; cutoff?: number }>({})

  useEffect(() => {
    api.listMovies().then(setMovies).catch(() => undefined)
    api.listSeries().then(setSeries).catch(() => undefined)
    api.getWanted('missing').then((r) => setCounts((c) => ({ ...c, missing: r.length }))).catch(() => undefined)
    api.getWanted('cutoff').then((r) => setCounts((c) => ({ ...c, cutoff: r.length }))).catch(() => undefined)
  }, [])
  useEffect(() => {
    if (!subtitlesOn) return setCounts((c) => ({ ...c, subtitles: undefined }))
    api.subtitlesWanted().then((r) => setCounts((c) => ({ ...c, subtitles: r.length }))).catch(() => undefined)
  }, [subtitlesOn])
  useEffect(() => {
    if (!musicOn) return setMusicCounts({})
    api.musicWanted('missing').then((r) => setMusicCounts((c) => ({ ...c, missing: r.length }))).catch(() => undefined)
    api.musicWanted('cutoff').then((r) => setMusicCounts((c) => ({ ...c, cutoff: r.length }))).catch(() => undefined)
  }, [musicOn])

  const posterOf = useMemo(() => {
    const byMovie = new Map(movies.map((m) => [m.tmdbId, m.posterUrl]))
    const bySeries = new Map(series.map((s) => [s.id, s.posterUrl]))
    return (i: { kind: string; tmdbId?: number; seriesId?: number }) => (i.kind === 'movie' ? byMovie.get(i.tmdbId ?? -1) : bySeries.get(i.seriesId ?? -1))
  }, [movies, series])

  const load = useCallback(() => {
    setError('')
    if (kind === 'subtitles') {
      setSubs(null)
      api.subtitlesWanted(showDismissed).then(setSubs).catch((e) => setError(e instanceof Error ? e.message : String(e)))
      api.subtitleQuota().then(setQuota).catch(() => undefined)
      return
    }
    setItems(null)
    setMusic(null)
    api.getWanted(kind).then(setItems).catch((e) => setError(e instanceof Error ? e.message : String(e)))
    if (musicOn) api.musicWanted(kind).then(setMusic).catch(() => setMusic([]))
  }, [kind, showDismissed, musicOn])
  useEffect(load, [load])
  // Quiet refresh: update the lists in place without flashing the loading state.
  useLive(() => {
    if (kind === 'subtitles') {
      api
        .subtitlesWanted(showDismissed)
        .then((r) => {
          setSubs(r)
          setError('')
        })
        .catch(() => undefined)
      api.subtitleQuota().then(setQuota).catch(() => undefined)
    } else {
      api
        .getWanted(kind)
        .then((r) => {
          setItems(r)
          setError('')
        })
        .catch(() => undefined)
      if (musicOn) api.musicWanted(kind).then(setMusic).catch(() => undefined)
    }
  }, 10000)

  const subKey = (i: { kind: string; id: number }) => `${i.kind}-${i.id}`
  const togglePick = (k: string) =>
    setPicked((cur) => {
      const n = new Set(cur)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })

  async function getSubtitles(all: boolean) {
    setSweeping(true)
    try {
      const items = (subs ?? []).filter((i) => picked.has(subKey(i)) && !i.dismissed).map((i) => ({ kind: i.kind, id: i.id }))
      const r = await api.subtitlesGet(all ? { all: true } : { items })
      setQuota(r.quota)
      if (r.stopped) toast.info(r.message)
      else toast.success(r.message)
      setPicked(new Set())
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setSweeping(false)
    }
  }

  async function dismiss(undo: boolean) {
    const items = (subs ?? []).filter((i) => picked.has(subKey(i))).map((i) => ({ kind: i.kind, id: i.id }))
    if (items.length === 0) return
    try {
      await (undo ? api.undismissSubtitles(items) : api.dismissSubtitles(items))
      toast.success(undo ? 'Back on the list.' : 'Marked as not needed.')
      setPicked(new Set())
      load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const keyOf = (i: WantedItem) => `${i.kind}-${i.id}`

  async function searchNow(item: WantedItem) {
    setBusy(keyOf(item))
    try {
      const res =
        item.kind === 'movie'
          ? await api.searchNowMovie(item.movieId ?? item.id)
          : await api.searchNowSeries(item.seriesId!, { season: item.season, episode: item.episode })
      toast.info(`${item.title}${item.subtitle ? ' ' + item.subtitle.split(' · ')[0] : ''}: ${res.message}`)
      if (res.grabbed > 0) load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const link = (i: { kind: string; tmdbId?: number; seriesId?: number }) => (i.kind === 'movie' ? `/title/${i.tmdbId}` : `/series/${i.seriesId}`)
  const missingState = describeState('searching')

  async function searchMusicNow(a: MusicWanted) {
    setBusy(`music-${a.id}`)
    try {
      const res = await api.searchNowAlbum(a.id)
      toast.info(`${a.artistName} – ${a.title}: ${res.message}`)
      if (res.grabbed > 0) load()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  const total = (k: Kind) => (counts[k] === undefined ? undefined : counts[k]! + (k === 'subtitles' ? 0 : (musicCounts[k] ?? 0)))
  const tab = (k: Kind, label: string) => (
    <button className={kind === k ? 'active' : ''} onClick={() => setKind(k)}>
      {label} {total(k) !== undefined && <small>({total(k)})</small>}
    </button>
  )

  // Which kinds of media to list, when more than one is switched on.
  const sel = media === 'all' || kinds.includes(media) ? media : 'all'
  const shownItems = (items ?? []).filter((i) => sel === 'all' || (sel === 'movie' && i.kind === 'movie') || (sel === 'tv' && i.kind === 'episode'))
  const shownMusic = sel === 'all' || sel === 'music' ? (music ?? []) : []
  const loadingList = items === null || (musicOn && music === null)
  const kindChips = kinds.length > 1 && kind !== 'subtitles' && (
    <div className="chip-row" style={{ marginBottom: 16, alignItems: 'center' }}>
      {(['all', ...kinds] as const).map((k) => (
        <button key={k} className={`chip${sel === k ? ' active' : ''}`} onClick={() => setMedia(k)}>
          {k !== 'all' && <Icon name={KIND_ICON[k]} size={13} />} {k === 'all' ? 'All' : KIND_LABEL[k]}
        </button>
      ))}
      {sel !== 'all' && (
        <button className="btn-sm" onClick={() => setMedia('all')}>
          Clear filters
        </button>
      )}
    </div>
  )

  return (
    <div>
      <div className="page-header">
        <h1>Wanted</h1>
        <div className="seg">
          {tab('missing', 'Missing')}
          {tab('cutoff', 'Upgrades')}
          {subtitlesOn && tab('subtitles', 'Subtitles')}
        </div>
      </div>

      {error && <p className="error-text">{error}</p>}
      {kindChips}

      {kind === 'subtitles' ? (
        subs === null ? (
          error ? null : <Loading height={120} />
        ) : (
          <>
            <QuotaNote quota={quota} />
            <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
              Downloaded titles with no subtitle in your languages. Pick the ones you want, or get them all, up to today&apos;s limit.
            </p>
            <div className="toolbar">
              <button className="btn-sm" onClick={() => setPicked(new Set(subs.filter((i) => !i.dismissed).map(subKey)))}>
                Select all
              </button>
              <button className="primary btn-with-icon" onClick={() => void getSubtitles(false)} disabled={sweeping || picked.size === 0}>
                <Icon name="download" size={16} /> {sweeping ? 'Working…' : picked.size > 0 ? `Get subtitles for ${picked.size} selected` : 'Get subtitles for selected'}
              </button>
              <button className="btn-with-icon" onClick={() => void getSubtitles(true)} disabled={sweeping || subs.filter((i) => !i.dismissed).length === 0}>
                Get all
              </button>
              <span className="spacer" />
              {admin && (
                <button className="btn-sm" onClick={() => void dismiss(showDismissed && subs.some((i) => picked.has(subKey(i)) && i.dismissed))} disabled={picked.size === 0}>
                  {showDismissed && subs.some((i) => picked.has(subKey(i)) && i.dismissed) ? 'Put back on the list' : 'Not needed'}
                </button>
              )}
              <label className="check-row" style={{ margin: 0 }}>
                <input type="checkbox" checked={showDismissed} onChange={(e) => setShowDismissed(e.target.checked)} /> Show titles marked as not needed
              </label>
            </div>
            {subs.length === 0 ? (
              <div className="empty-state">
                <Icon name="check" size={44} />
                <p>Every downloaded title has all its subtitles.</p>
              </div>
            ) : (
              <div className="wlist">
                {subs.map((s) => (
                  <div key={subKey(s)} className={`wrow kind-${s.kind === 'movie' ? 'movie' : 'tv'}${s.dismissed ? ' dismissed' : ''}`}>
                    <label className="pick-box" title="Select">
                      <input type="checkbox" checked={picked.has(subKey(s))} onChange={() => togglePick(subKey(s))} />
                    </label>
                    <div className="qthumb">{posterOf(s) ? <img src={posterOf(s)} alt="" loading="lazy" /> : <PosterFallback />}</div>
                    <div className="wmain">
                      <Link to={link(s)}>
                        <strong>{s.title}</strong>
                      </Link>
                      {s.subtitle && <small>{s.subtitle}</small>}
                    </div>
                    <div className="wmeta">
                      {s.dismissed && <span className="badge">not needed</span>}
                      {s.missing.map((m) => (
                        <span key={m} className="badge missing">
                          {languageName(m)}
                        </span>
                      ))}
                    </div>
                    <Link className="icon-btn" to={link(s)} title="Open" aria-label="Open">
                      <Icon name="open" size={17} />
                    </Link>
                  </div>
                ))}
              </div>
            )}
          </>
        )
      ) : loadingList ? (
        error ? null : <Loading height={120} />
      ) : shownItems.length === 0 && shownMusic.length === 0 ? (
        <div className="empty-state">
          <Icon name="check" size={44} />
          {sel !== 'all' && (items?.length ?? 0) + (music?.length ?? 0) > 0 ? (
            <>
              <p>Nothing matches this filter.</p>
              <button className="btn-with-icon" onClick={() => setMedia('all')}>
                Clear filters
              </button>
            </>
          ) : (
            <p>{kind === 'missing' ? "Nothing is missing. Everything you're monitoring is downloaded or hasn't come out yet." : "Everything you've downloaded is already at the quality you asked for."}</p>
          )}
        </div>
      ) : (
        <div className="wlist">
          {shownItems.map((i) => (
            <div key={keyOf(i)} className={`wrow kind-${i.kind === 'movie' ? 'movie' : 'tv'}`}>
              <div className="qthumb">{posterOf(i) ? <img src={posterOf(i)} alt="" loading="lazy" /> : <PosterFallback />}</div>
              <div className="wmain">
                <Link to={link(i)}>
                  <strong>
                    <Icon name={i.kind === 'movie' ? 'film' : 'tv'} size={13} /> {i.title}
                  </strong>
                </Link>
                {i.subtitle && <small>{i.subtitle}</small>}
              </div>
              <div className="wmeta">
                {kind === 'missing' ? (
                  <span className={`state-tag st-${missingState.key}`} title={missingState.hint}>
                    <Icon name={missingState.icon} size={12} /> {missingState.label}
                  </span>
                ) : (
                  <span className="state-tag st-partial" title="Downloaded, but below the quality you asked for">
                    <Icon name="star" size={12} /> {i.quality} → {i.cutoff}
                  </span>
                )}
                {kind === 'missing' && i.date && <small>{i.date}</small>}
                {i.profileName && <span className="badge">{i.profileName}</span>}
              </div>
              <div className="row-actions">
                <button className="primary btn-sm btn-with-icon" disabled={busy === keyOf(i)} onClick={() => void searchNow(i)}>
                  <Icon name="search" size={14} /> {busy === keyOf(i) ? 'Searching…' : 'Search now'}
                </button>
                <Link className="icon-btn" to={link(i)} title="Open" aria-label="Open">
                  <Icon name="open" size={17} />
                </Link>
              </div>
            </div>
          ))}
          {shownMusic.map((a) => {
            const to = `/music/artist/${a.artistId}#album-${a.id}`
            return (
              <div key={`music-${a.id}`} className="wrow kind-music">
                <div className="qthumb">
                  <Cover src={a.coverUrl} />
                </div>
                <div className="wmain">
                  <Link to={to}>
                    <strong>
                      <Icon name="music" size={13} /> {a.artistName} – {a.title}
                    </strong>
                  </Link>
                  <small>{[a.year || null, a.type === 'ep' ? 'EP' : a.type === 'single' ? 'Single' : 'Album'].filter(Boolean).join(' · ')}</small>
                </div>
                <div className="wmeta">
                  {kind === 'missing' ? (
                    <span className={`state-tag st-${missingState.key}`} title={a.lastSearch || missingState.hint}>
                      <Icon name={missingState.icon} size={12} /> {missingState.label}
                    </span>
                  ) : (
                    <span className="state-tag st-partial" title="Downloaded, but below the quality you asked for">
                      <Icon name="star" size={12} /> {a.quality} → {a.cutoff}
                    </span>
                  )}
                  {a.profileName && <span className="badge">{a.profileName}</span>}
                </div>
                <div className="row-actions">
                  <button className="primary btn-sm btn-with-icon" disabled={busy === `music-${a.id}`} onClick={() => void searchMusicNow(a)}>
                    <Icon name="search" size={14} /> {busy === `music-${a.id}` ? 'Searching…' : 'Search now'}
                  </button>
                  <Link className="icon-btn" to={to} title="Open" aria-label="Open">
                    <Icon name="open" size={17} />
                  </Link>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
