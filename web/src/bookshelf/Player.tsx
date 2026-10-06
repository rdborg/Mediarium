import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Book, type BookTrack } from '../api'
import Icon from '../components/Icon'
import { PosterFallback } from '../components/PosterCard'
import { useDocumentTitle } from '../documentTitle'
import { buildChapters, chapterAt, formatAudioPosition, formatClock, nextSpeed, overallPercent, parseAudioPosition } from './shelfMath'

const SPEED_KEY = 'mediarium.player.speed'
const SLEEP_CHOICES = [0, 15, 30, 45, 60, -1] // minutes; -1 = end of this chapter

function savedSpeed(): number {
  try {
    const v = Number(localStorage.getItem(SPEED_KEY))
    return v >= 0.5 && v <= 3 ? v : 1
  } catch {
    return 1
  }
}

// The audiobook player: one book, its chapters in order (the audio files, or
// the chapter marks inside a single M4B file), with skips, speed, a sleep
// timer and the phone's lock-screen controls. Your
// place is kept on the server, so the book carries on where you stopped on
// any device.
export default function Player() {
  const { id } = useParams()
  const bookId = Number(id)
  const audio = useRef<HTMLAudioElement>(null)
  const [book, setBook] = useState<Book | null>(null)
  const [tracks, setTracks] = useState<BookTrack[] | null>(null)
  const [error, setError] = useState('')
  const [track, setTrack] = useState(0)
  const [time, setTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(savedSpeed)
  const [sleep, setSleep] = useState(0) // minutes left on the timer; -1 = end of chapter
  const [sleepEnds, setSleepEnds] = useState(0)
  const [showChapters, setShowChapters] = useState(false)
  const resumeAt = useRef(0) // seconds to jump to once the track has loaded
  const lastSaved = useRef(0)
  useDocumentTitle(book ? `${book.title} · Listening` : 'Listening')

  useEffect(() => {
    let stale = false
    Promise.all([api.getBook(bookId), api.bookTracks(bookId), api.bookProgress(bookId, 'audiobook').catch(() => null)])
      .then(([b, t, p]) => {
        if (stale) return
        setBook(b)
        setTracks(t.tracks)
        const at = parseAudioPosition(p?.position)
        if (at.track < t.tracks.length) {
          setTrack(at.track)
          resumeAt.current = at.seconds
          setTime(at.seconds)
        }
      })
      .catch((e) => !stale && setError(e instanceof Error ? e.message : String(e)))
    return () => {
      stale = true
    }
  }, [bookId])

  const sizes = (tracks ?? []).map((t) => t.size)
  const percent = overallPercent(sizes, track, duration > 0 ? time / duration : 0)
  const chapters = useMemo(() => buildChapters(tracks ?? []), [tracks])
  const cur = chapterAt(chapters, track, time)
  const chapter = chapters[cur]
  const chStart = chapter?.start ?? 0
  const chEnd = chapter?.end ?? duration

  const save = useCallback(
    (keepalive = false) => {
      const a = audio.current
      if (!tracks || !a) return
      const secs = a.currentTime || resumeAt.current
      const pct = overallPercent(
        tracks.map((t) => t.size),
        track,
        a.duration > 0 ? secs / a.duration : 0,
      )
      const finished = track === tracks.length - 1 && a.duration > 0 && secs >= a.duration - 5
      void api.saveBookProgress(bookId, { format: 'audiobook', position: formatAudioPosition({ track, seconds: secs }), percent: pct, finished }, keepalive).catch(() => undefined)
      lastSaved.current = Date.now()
    },
    [bookId, track, tracks],
  )

  // Keep the place: every 15 seconds while playing, and when the page is hidden or closed.
  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === 'hidden') save(true)
    }
    document.addEventListener('visibilitychange', onHide)
    window.addEventListener('pagehide', onHide)
    return () => {
      document.removeEventListener('visibilitychange', onHide)
      window.removeEventListener('pagehide', onHide)
    }
  }, [save])

  useEffect(() => {
    if (audio.current) audio.current.playbackRate = speed
    try {
      localStorage.setItem(SPEED_KEY, String(speed))
    } catch {
      // not remembered, that's all
    }
  }, [speed])

  const play = useCallback(() => {
    void audio.current?.play().catch(() => setPlaying(false))
  }, [])
  const pause = useCallback(() => audio.current?.pause(), [])
  const seekBy = useCallback((s: number) => {
    const a = audio.current
    if (a) a.currentTime = Math.max(0, Math.min((a.duration || 0) - 0.5, a.currentTime + s))
  }, [])
  const goTrack = useCallback(
    (n: number, autoplay = true, at = 0) => {
      if (!tracks || n < 0 || n >= tracks.length) return
      save()
      resumeAt.current = at
      setTime(at)
      setTrack(n)
      if (autoplay) window.setTimeout(play, 0)
    },
    [tracks, play, save],
  )
  // goChapter jumps to the start of chapter n: within the file that is
  // playing, or by opening the file it is in.
  const goChapter = useCallback(
    (n: number) => {
      const c = chapters[n]
      if (!c) return
      const a = audio.current
      if (c.track === track && a) {
        a.currentTime = c.start
        setTime(c.start)
        play()
        return
      }
      goTrack(c.track, true, c.start)
    },
    [chapters, track, goTrack, play],
  )

  // The lock screen and headphone buttons.
  useEffect(() => {
    if (!('mediaSession' in navigator) || !book || !tracks) return
    navigator.mediaSession.metadata = new MediaMetadata({
      title: chapter?.title ?? book.title,
      artist: book.author,
      album: book.title,
      artwork: book.coverUrl ? [{ src: book.coverUrl.replace('-M.jpg', '-L.jpg'), sizes: '500x750', type: 'image/jpeg' }] : [],
    })
    const ms = navigator.mediaSession
    ms.setActionHandler('play', play)
    ms.setActionHandler('pause', pause)
    ms.setActionHandler('seekbackward', () => seekBy(-30))
    ms.setActionHandler('seekforward', () => seekBy(30))
    ms.setActionHandler('previoustrack', () => goChapter(cur - 1))
    ms.setActionHandler('nexttrack', () => goChapter(cur + 1))
    return () => {
      for (const a of ['play', 'pause', 'seekbackward', 'seekforward', 'previoustrack', 'nexttrack'] as MediaSessionAction[]) ms.setActionHandler(a, null)
    }
  }, [book, tracks, chapter, cur, play, pause, seekBy, goChapter])

  // The sleep timer.
  useEffect(() => {
    if (sleep <= 0 || !sleepEnds) return
    const t = window.setInterval(() => {
      if (Date.now() >= sleepEnds) {
        pause()
        setSleep(0)
        setSleepEnds(0)
      }
    }, 1000)
    return () => window.clearInterval(t)
  }, [sleep, sleepEnds, pause])

  // Space plays and pauses; the arrows skip.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement)?.tagName === 'INPUT') return
      if (e.key === ' ') {
        e.preventDefault()
        if (audio.current?.paused) play()
        else pause()
      } else if (e.key === 'ArrowLeft') seekBy(-30)
      else if (e.key === 'ArrowRight') seekBy(30)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [play, pause, seekBy])

  function chooseSleep() {
    const i = SLEEP_CHOICES.indexOf(sleep)
    const next = SLEEP_CHOICES[(i + 1) % SLEEP_CHOICES.length]
    setSleep(next)
    setSleepEnds(next > 0 ? Date.now() + next * 60_000 : 0)
  }

  if (error) {
    return (
      <div className="bks-message">
        <Icon name="headphones" size={44} />
        <h1>Couldn't open this audiobook</h1>
        <p>{error}</p>
        <Link className="btn-with-icon" to="/bookshelf">
          <Icon name="chevron-left" size={16} /> Back to your books
        </Link>
      </div>
    )
  }
  if (!book || !tracks) return <div className="bks-loading">Opening…</div>
  if (tracks.length === 0) {
    return (
      <div className="bks-message">
        <Icon name="headphones" size={44} />
        <h1>{book.title}</h1>
        <p>No audio files were found for this audiobook.</p>
        <Link className="btn-with-icon" to="/bookshelf">
          <Icon name="chevron-left" size={16} /> Back to your books
        </Link>
      </div>
    )
  }

  const left = sleep > 0 ? Math.max(0, Math.ceil((sleepEnds - Date.now()) / 60_000)) : 0
  return (
    <div className="ply">
      <header className="rdr-top ply-top">
        <Link className="icon-btn" to="/bookshelf" title="Back to your books" aria-label="Back to your books">
          <Icon name="chevron-left" size={20} />
        </Link>
        <span className="rdr-title">
          <strong>{book.title}</strong>
          <small>{[book.author, book.narrators && `read by ${book.narrators.split(',').slice(0, 2).join(',').trim()}`].filter(Boolean).join(' · ')}</small>
        </span>
        <button className={`icon-btn${showChapters ? ' active' : ''}`} onClick={() => setShowChapters((v) => !v)} title="Chapters" aria-label="Chapters">
          <Icon name="list" size={18} />
        </button>
      </header>

      <div className={`ply-body${showChapters ? ' with-list' : ''}`}>
        <div className="ply-main">
          <div className="ply-cover">{book.coverUrl ? <img src={book.coverUrl.replace('-M.jpg', '-L.jpg')} alt="" /> : <PosterFallback />}</div>
          <div className="ply-chapter">
            <strong>{chapter?.title}</strong>
            <small>
              Chapter {cur + 1} of {chapters.length} · {Math.round(percent)}% of the book
            </small>
          </div>
          <div className="ply-scrub">
            <input
              type="range"
              min={chStart}
              max={chEnd || chStart + 1}
              step={1}
              value={duration ? Math.min(Math.max(time, chStart), chEnd) : chStart}
              disabled={!duration}
              onChange={(e) => {
                const v = Number(e.target.value)
                setTime(v)
                if (audio.current) audio.current.currentTime = v
              }}
              aria-label="Place in this chapter"
            />
            <div className="ply-times">
              <span>{formatClock(Math.max(0, time - chStart))}</span>
              <span>{duration ? `-${formatClock(Math.max(0, (chEnd - time) / speed))}` : '–'}</span>
            </div>
          </div>
          <div className="ply-controls">
            <button className="icon-btn" onClick={() => goChapter(cur - 1)} disabled={cur === 0} title="Previous chapter" aria-label="Previous chapter">
              <Icon name="chevron-left" size={22} />
            </button>
            <button className="ply-skip" onClick={() => seekBy(-30)} title="Back 30 seconds" aria-label="Back 30 seconds">
              −30
            </button>
            <button className="ply-play" onClick={() => (playing ? pause() : play())} aria-label={playing ? 'Pause' : 'Play'}>
              <Icon name={playing ? 'pause' : 'play'} size={30} />
            </button>
            <button className="ply-skip" onClick={() => seekBy(30)} title="Forward 30 seconds" aria-label="Forward 30 seconds">
              +30
            </button>
            <button className="icon-btn" onClick={() => goChapter(cur + 1)} disabled={cur >= chapters.length - 1} title="Next chapter" aria-label="Next chapter">
              <Icon name="chevron-right" size={22} />
            </button>
          </div>
          <div className="ply-extras">
            <button className="btn-sm" onClick={() => setSpeed((s) => nextSpeed(s))} title="Playback speed">
              {speed}×
            </button>
            <button className={`btn-sm btn-with-icon${sleep !== 0 ? ' primary' : ''}`} onClick={chooseSleep} title="Sleep timer">
              <Icon name="moon" size={14} /> {sleep === 0 ? 'Sleep timer' : sleep === -1 ? 'End of chapter' : `${left} min`}
            </button>
          </div>
        </div>

        {showChapters && (
          <ol className="ply-list">
            {chapters.map((c, i) => (
              <li key={`${c.track}-${c.start}`}>
                <button className={i === cur ? 'active' : ''} onClick={() => goChapter(i)}>
                  <span className="ply-n">{i + 1}</span> {c.title}
                </button>
              </li>
            ))}
          </ol>
        )}
      </div>

      <audio
        ref={audio}
        src={api.bookTrackUrl(book.id, track)}
        preload="metadata"
        onLoadedMetadata={(e) => {
          const a = e.currentTarget
          setDuration(a.duration)
          a.playbackRate = speed
          if (resumeAt.current > 0 && resumeAt.current < a.duration) a.currentTime = resumeAt.current
          resumeAt.current = 0
        }}
        onTimeUpdate={(e) => {
          const a = e.currentTarget
          setTime(a.currentTime)
          // "End of chapter" inside one file: stop where the next chapter starts.
          if (sleep === -1 && chapter?.end !== undefined && a.currentTime >= chapter.end - 0.3 && !a.paused) {
            a.pause()
            a.currentTime = chapter.end
            setSleep(0)
          }
          if (!a.paused && Date.now() - lastSaved.current > 15_000) save()
        }}
        onPlay={() => setPlaying(true)}
        onPause={() => {
          setPlaying(false)
          save()
        }}
        onEnded={() => {
          if (sleep === -1) {
            setSleep(0)
            setPlaying(false)
            save()
            return
          }
          if (track < tracks.length - 1) goTrack(track + 1)
          else {
            setPlaying(false)
            save()
          }
        }}
      />
    </div>
  )
}
