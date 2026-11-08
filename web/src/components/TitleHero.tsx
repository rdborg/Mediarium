import { useEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react'
import { api, type CastMember, type OutsideScores, type Trailer } from '../api'
import { plural, t } from '../i18n'
import { trailerLabels } from '../trailerLabels'
import { trailerAllowed, trailerEmbedUrl, trailerLog, trailerLogReset, trailerProblem, useTrailerBackground, useTrailerSound } from '../trailerBackground'
import { useModules } from '../ModulesContext'
import { pictureSrcSet } from '../base'
import Icon from './Icon'
import TrailerDialog from './TrailerDialog'
import { PosterFallback } from './PosterCard'
import { PeopleLinks } from './TitlePreview'

export interface HeroInfo {
  kind: 'movie' | 'tv'
  title: string
  year?: number
  posterUrl?: string
  backdropUrl?: string
  genres?: string[]
  rating?: number
  voteCount?: number
  runtime?: number
  certification?: string
  tagline?: string
  overview?: string
  trailers?: Trailer[]
  cast?: CastMember[]
  facts?: { label: string; value: ReactNode }[]
  links?: { label: string; url: string }[] // the film's website, IMDb, TMDB
  // With both, the IMDb, Rotten Tomatoes and Metacritic scores are asked for
  // (they only come when an OMDb key is saved).
  tmdbId?: number
  imdbId?: string
}

// The IMDb, Rotten Tomatoes and Metacritic scores, next to TMDB's star (#169).
// Nothing shows without an OMDb key or when OMDb knows none.
function ScoreChips({ kind, tmdbId, imdbId }: { kind: 'movie' | 'tv'; tmdbId?: number; imdbId?: string }) {
  const [scores, setScores] = useState<OutsideScores | null>(null)
  useEffect(() => {
    setScores(null)
    if (!tmdbId || !imdbId) return
    let live = true
    api
      .titleScores(kind, tmdbId, imdbId)
      .then((r) => live && setScores(r.scores))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [kind, tmdbId, imdbId])
  if (!scores) return null
  return <OutsideScoreChips scores={scores} />
}

export function OutsideScoreChips({ scores, small }: { scores: OutsideScores; small?: boolean }) {
  return (
    <>
      {scores.imdb ? (
        <span className={`chip-score${small ? ' small' : ''}`} title={scores.imdbVotes ? plural(scores.imdbVotes, 'IMDb, {n} vote', 'IMDb, {n} votes') : 'IMDb'}>
          <b>IMDb</b> {scores.imdb.toFixed(1)}
        </span>
      ) : null}
      {scores.rottenTomatoes ? (
        <span className={`chip-score rt${small ? ' small' : ''}`} title={t('Rotten Tomatoes, critics')}>
          <b>RT</b> {scores.rottenTomatoes}%
        </span>
      ) : null}
      {scores.metacritic && !small ? (
        <span className="chip-score" title="Metacritic">
          <b>MC</b> {scores.metacritic}
        </span>
      ) : null}
    </>
  )
}

function runtimeLabel(min?: number): string {
  if (!min) return ''
  const h = Math.floor(min / 60)
  const m = min % 60
  return h ? t('{h}h {m}m', { h, m }) : t('{m}m', { m })
}

// The top of a movie or show page: poster on the left, everything worth
// knowing stacked on the right (title, rating, age rating, genres, summary,
// trailer links, cast), with the page's own action buttons passed in.
export default function TitleHero({ info, status, actions, children }: { info: HeroInfo; status?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  const side = useRef<HTMLDivElement>(null)
  // The poster turns a little towards the pointer and catches the light.
  function tilt(e: PointerEvent<HTMLDivElement>) {
    const el = side.current
    if (!el || e.pointerType !== 'mouse' || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const r = el.getBoundingClientRect()
    const x = (e.clientX - r.left) / r.width
    const y = (e.clientY - r.top) / r.height
    el.style.setProperty('--ry', `${((x - 0.5) * 14).toFixed(2)}deg`)
    el.style.setProperty('--rx', `${((0.5 - y) * 10).toFixed(2)}deg`)
    el.style.setProperty('--gx', `${(x * 100).toFixed(1)}%`)
    el.style.setProperty('--gy', `${(y * 100).toFixed(1)}%`)
    el.classList.add('tilting')
  }
  function untilt() {
    const el = side.current
    if (!el) return
    el.style.setProperty('--rx', '0deg')
    el.style.setProperty('--ry', '0deg')
    el.classList.remove('tilting')
  }
  const trailers = (info.trailers ?? []).slice(0, 3)
  // The trailer open in a box over the page (the index in the list of trailers shown).
  const [trailerOpen, setTrailerOpen] = useState<number | null>(null)
  const { trailerOn, trailerSoundOn } = useModules()
  const [bgOn, setBgOn] = useTrailerBackground(trailerOn)
  // Whether sound is wanted (the server's choice, or this browser's), and whether the video really has it now.
  const [soundWanted, setSoundWanted] = useTrailerSound(trailerSoundOn)
  const wantSound = useRef(soundWanted)
  wantSound.current = soundWanted
  const [soundOn, setSoundOn] = useState(false)
  // Sound was asked for but the browser wants a tap or a key press first (a page opened straight from a link or a refresh).
  const [awaitingTap, setAwaitingTap] = useState(false)
  const [videoReady, setVideoReady] = useState(false)
  // The scene is as wide as the whole page while the video shows, and goes back to its own width a moment after the video has faded out.
  const [wide, setWide] = useState(false)
  useEffect(() => {
    if (videoReady) {
      setWide(true)
      return
    }
    const timer = window.setTimeout(() => setWide(false), 1600)
    return () => window.clearTimeout(timer)
  }, [videoReady])
  // The trailer plays once; when it ends the still backdrop comes back.
  const [ended, setEnded] = useState(false)
  const frame = useRef<HTMLIFrameElement>(null)
  // The player is laid out as a full 1080p screen and scaled down to cover the scene, so YouTube believes the player
  // is big and sends its best picture; shrinking it in CSS alone would let it settle for a smaller one.
  const scene = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = scene.current
    if (!el) return
    const fit = () => {
      const { width, height } = el.getBoundingClientRect()
      if (width > 0 && height > 0) el.style.setProperty('--vscale', String(Math.max(width / 1920, height / 1080) * 1.32))
    }
    fit()
    const watch = new ResizeObserver(fit)
    watch.observe(el)
    return () => watch.disconnect()
  }, [info.backdropUrl])
  const canPlay = trailers.length > 0 && trailerAllowed()
  const playing = bgOn && canPlay
  // The first trailer YouTube will play: when one can't be embedded, or doesn't
  // start, the next one is tried, and when none plays the still backdrop stays.
  const [tIndex, setTIndex] = useState(0)
  const [why, setWhy] = useState('') // what went wrong with the last trailer tried: YouTube's error code, or 'timeout'
  const trailerKey = trailers[tIndex]?.key
  // The video starts a moment after the page, so the still backdrop shows first.
  const [started, setStarted] = useState(false)
  useEffect(() => {
    if (!playing) {
      setStarted(false)
      setVideoReady(false)
      setSoundOn(false)
      setAwaitingTap(false)
      setEnded(false)
      setTIndex(0)
      setWhy('')
      return
    }
    trailerLogReset()
    const onVisible = () => trailerLog(`tab ${document.visibilityState}`)
    document.addEventListener('visibilitychange', onVisible)
    const timer = window.setTimeout(() => setStarted(true), 1200)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [playing])
  // Listens to the player: it shows once it is really playing, and a video that
  // fails or never starts is swapped for the next trailer.
  useEffect(() => {
    if (!playing || !started || !trailerKey || ended) return
    trailerLog(`trailer ${tIndex + 1} of ${trailers.length} added (${trailerKey})`)
    setVideoReady(false)
    const next = () => setTIndex((i) => i + 1)
    // A page in a background tab can't play video, so the wait only counts while it is shown.
    let giveUp = 0
    const arm = () => {
      giveUp = window.setTimeout(() => {
        if (document.hidden) return arm()
        trailerLog('gave up waiting, trying the next trailer')
        setWhy('timeout')
        next()
      }, 10000)
    }
    arm()
    // Set once the trailer is in its last seconds, so the still image is already
    // coming back while the video finishes.
    let closing = false
    let duration = 0
    let nudges = 0 // times the player was asked to start again
    let soundTried = false
    let unmutedAt = 0 // when the sound was switched on by itself, to notice a browser that pauses the video for it
    const onMessage = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow || typeof e.data !== 'string') return
      let msg: { event?: string; info?: unknown }
      try {
        msg = JSON.parse(e.data)
      } catch {
        return
      }
      if (msg.event !== 'infoDelivery' || (msg.info as { playerState?: number } | undefined)?.playerState !== undefined) {
        trailerLog(`player ${msg.event}${msg.event === 'onError' ? ' ' + String(msg.info) : ''}${(msg.info as { playerState?: number } | undefined)?.playerState !== undefined ? ' state=' + (msg.info as { playerState?: number }).playerState : ''}`)
      }
      const delivery = msg.event === 'infoDelivery' ? (msg.info as { playerState?: number; currentTime?: number; duration?: number } | undefined) : undefined
      const state = msg.event === 'onStateChange' ? msg.info : delivery?.playerState
      if (delivery?.duration) duration = delivery.duration
      if (!closing && duration > 0 && delivery?.currentTime !== undefined && duration - delivery.currentTime <= 2.5) {
        closing = true
        setVideoReady(false)
      }
      if (msg.event === 'onReady') {
        // Autoplay alone doesn't always start a player that was just loaded.
        command('playVideo')
      } else if (msg.event === 'onError') {
        window.clearTimeout(giveUp)
        setWhy(String(msg.info))
        next()
      } else if (state === 0) {
        window.clearTimeout(giveUp)
        setVideoReady(false)
        setEnded(true)
      } else if (state === 2 && unmutedAt > 0 && Date.now() - unmutedAt < 3000) {
        // The browser did not allow sound without a click: back to muted, and carry on playing.
        unmutedAt = 0
        trailerLog('sound was not allowed, playing muted')
        command('mute')
        command('playVideo')
        setSoundOn(false)
        setAwaitingTap(true)
      } else if ((state === -1 || state === 5) && nudges < 6) {
        // A player that is ready but hasn't started is asked again, muted, a few times.
        nudges++
        window.setTimeout(() => {
          command('mute')
          command('playVideo')
        }, 1200)
      } else if (state === 1 && !closing) {
        window.clearTimeout(giveUp)
        setVideoReady(true)
        command('setPlaybackQuality', ['hd1080'])
        if (wantSound.current && unmutedAt === 0 && !soundTried) {
          soundTried = true
          // Without any tap on this page the browser refuses (and pauses the video), so wait for the first one instead.
          if (typeof navigator !== 'undefined' && navigator.userActivation && !navigator.userActivation.hasBeenActive) {
            setAwaitingTap(true)
          } else {
            unmutedAt = Date.now()
            command('unMute')
            setSoundOn(true)
          }
        }
      }
    }
    window.addEventListener('message', onMessage)
    return () => {
      window.clearTimeout(giveUp)
      window.removeEventListener('message', onMessage)
    }
  }, [playing, started, trailerKey, ended])
  function command(func: 'mute' | 'unMute' | 'playVideo' | 'setPlaybackQuality', args: unknown[] = []) {
    frame.current?.contentWindow?.postMessage(JSON.stringify({ event: 'command', func, args }), '*')
  }
  // Pressing a trailer button hands over to the pop-up: the backdrop video is
  // muted, the still picture fades back in, and the video is taken away once that has happened.
  function stopBackground() {
    if (!playing || ended) return
    trailerLog('stopped for the pop-up')
    command('mute')
    setSoundOn(false)
    setVideoReady(false)
    window.setTimeout(() => setEnded(true), 1500)
  }
  function toggleSound() {
    command(soundOn ? 'mute' : 'unMute')
    setSoundOn(!soundOn)
    setSoundWanted(!soundOn)
    setAwaitingTap(false)
  }
  // The first tap or key press anywhere on the page gives the browser the go-ahead for sound.
  useEffect(() => {
    if (!awaitingTap || !playing) return
    const go = () => {
      if (!wantSound.current) return
      command('unMute')
      setSoundOn(true)
      setAwaitingTap(false)
    }
    window.addEventListener('pointerdown', go, { once: true, capture: true })
    window.addEventListener('keydown', go, { once: true, capture: true })
    return () => {
      window.removeEventListener('pointerdown', go, { capture: true })
      window.removeEventListener('keydown', go, { capture: true })
    }
  }, [awaitingTap, playing])
  const trailerNames = trailerLabels(trailers)
  const facts = [
    info.year ? { label: t('Year'), value: String(info.year) } : null,
    info.runtime ? { label: t('Runtime'), value: runtimeLabel(info.runtime) } : null,
    ...(info.facts ?? []),
  ].filter(Boolean) as { label: string; value: ReactNode }[]

  return (
    <section className={`title-hero kind-${info.kind}`}>
      {info.backdropUrl ? (
        <div ref={scene} className={`title-scene${videoReady ? ' video-on' : ''}${wide ? ' video-wide' : ''}`} aria-hidden="true">
          <img src={info.backdropUrl} srcSet={pictureSrcSet(info.backdropUrl, [780, 1280])} sizes="100vw" alt="" />
          {playing && started && trailerKey && !ended && (
            <iframe
              key={trailerKey}
              ref={frame}
              className={`scene-video${videoReady ? ' ready' : ''}`}
              src={trailerEmbedUrl(trailerKey)}
              title=""
              tabIndex={-1}
              allow="autoplay; encrypted-media"
              referrerPolicy="strict-origin-when-cross-origin"
              onLoad={(e) => {
                trailerLog('player page loaded')
                e.currentTarget.contentWindow?.postMessage(JSON.stringify({ event: 'listening', id: 1, channel: 'widget' }), '*')
              }}
            />
          )}
        </div>
      ) : (
        info.posterUrl && <div className="title-hero-bg" style={{ backgroundImage: `url("${encodeURI(info.posterUrl).replace(/"/g, '%22')}")` }} aria-hidden="true" />
      )}
      <div className="title-side" ref={side} onPointerMove={tilt} onPointerLeave={untilt}>
        <div className="title-poster">
          {info.posterUrl ? <img src={info.posterUrl} alt={info.title} /> : <PosterFallback />}
        </div>
        {info.kind === 'tv' && (
          <div className="title-chin" aria-hidden="true">
            <span className="grille" />
            <span className="led" />
            <span className="knob" />
            <span className="knob" />
          </div>
        )}
        {info.kind === 'movie' && (
          <div className="title-stub" aria-hidden="true">
            <span className="stub-data">{[info.year, runtimeLabel(info.runtime)].filter(Boolean).join(' · ')}</span>
            <span className="stub-admit">{t('Admit one')}</span>
          </div>
        )}
      </div>
      <div className="title-info">
        <div className="title-kicker">
          <span className={`kind-tag kind-${info.kind}`}>
            <Icon name={info.kind === 'movie' ? 'film' : 'tv'} size={13} /> {info.kind === 'movie' ? t('Movie') : t('TV show')}
          </span>
          {status}
        </div>
        <h1>
          {info.title} {info.year ? <span className="title-year">({info.year})</span> : null}
        </h1>
        {info.tagline && <p className="title-tagline">{info.tagline}</p>}

        <div className="title-chips">
          {info.rating ? (
            <span className="chip-rate" title={info.voteCount ? plural(info.voteCount, '{n} vote', '{n} votes') : t('Rating')}>
              <Icon name="star" size={14} /> {info.rating.toFixed(1)}
              <small>/10</small>
            </span>
          ) : null}
          <ScoreChips kind={info.kind} tmdbId={info.tmdbId} imdbId={info.imdbId} />
          {info.certification && (
            <span className="chip-cert" title={t('Age classification')}>
              {info.certification}
            </span>
          )}
          {info.runtime ? (
            <span className="chip-plain">
              <Icon name="clock" size={13} /> {runtimeLabel(info.runtime)}
            </span>
          ) : null}
          {(info.genres ?? []).map((g) => (
            <span key={g} className="chip-genre">
              {g}
            </span>
          ))}
        </div>

        {info.overview && <p className="title-overview">{info.overview}</p>}

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

        {(trailers.length > 0 || (info.links ?? []).length > 0) && (
          <div className="title-links">
            {trailers.map((t, i) => (
              <a
                key={t.url}
                href={t.url}
                target="_blank"
                rel="noreferrer"
                className="link-pill"
                onClick={(e) => {
                  // A plain click plays it here; a click with Ctrl, Shift or the middle button still opens YouTube.
                  if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0 || !t.key) return
                  e.preventDefault()
                  stopBackground()
                  setTrailerOpen(i)
                }}
              >
                <Icon name="play" size={13} /> {trailerNames[i]}
              </a>
            ))}
            {canPlay && info.backdropUrl && (
              <button
                type="button"
                className={`link-pill pill-toggle quiet${bgOn ? ' on' : ''}${bgOn && trailerKey && !videoReady && !ended ? ' starting' : ''}`}
                aria-pressed={bgOn}
                aria-label={!bgOn ? t('Play the trailer behind the page') : trailerKey && !videoReady && !ended ? t('Trailer behind the page: starting…') : t('Trailer behind the page: on')}
                onClick={() => setBgOn(!bgOn)}
                title={(!bgOn ? t('Play the trailer behind the page') : trailerKey && !videoReady && !ended ? t('Trailer behind the page: starting…') : t('Trailer behind the page: on')) + '. ' + t('The video comes from YouTube, so YouTube sees the visit.')}
              >
                <Icon name="film" size={14} />
              </button>
            )}
            {playing && started && trailerKey && !videoReady && !ended && why && (
              <span className="trailer-note">{t("That trailer didn't start, trying the next one.")}</span>
            )}
            {playing && started && !trailerKey && trailers.length > 0 && <span className="trailer-note">{trailerProblem(why)}</span>}
            {playing && started && !ended && (
              <button type="button" className="link-pill pill-toggle quiet" aria-pressed={soundOn} aria-label={soundOn ? t('Sound on') : awaitingTap ? t('Sound is on once you tap or press a key') : t('Sound off')} title={soundOn ? t('Sound on') : awaitingTap ? t('Your browser wants a tap or a key press before it plays sound. The next one turns it on.') : t('Sound off')} onClick={toggleSound}>
                <Icon name={soundOn ? 'volume' : 'volume-off'} size={14} />
              </button>
            )}
            <a className="link-pill" href={`https://www.youtube.com/results?search_query=${encodeURIComponent(`${info.title} ${info.year ?? ''} review`)}`} target="_blank" rel="noreferrer">
              <Icon name="external" size={13} /> {t('Reviews')}
            </a>
            {(info.links ?? []).map((l) => (
              <a key={l.url} className="link-pill" href={l.url} target="_blank" rel="noreferrer">
                <Icon name="external" size={13} /> {l.label}
              </a>
            ))}
          </div>
        )}

        {info.cast && info.cast.length > 0 && (
          <p className="title-cast">
            <strong>{t('Starring')}</strong> <PeopleLinks people={info.cast.slice(0, 6)} />
          </p>
        )}

        {trailerOpen !== null && trailers[trailerOpen] && <TrailerDialog name={`${info.title} · ${trailerNames[trailerOpen]}`} videoKey={trailers[trailerOpen].key} watchUrl={trailers[trailerOpen].url} onClose={() => setTrailerOpen(null)} />}
        {actions && <div className="title-actions">{actions}</div>}
        {children}
      </div>
    </section>
  )
}
