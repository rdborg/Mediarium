import { createPortal } from 'react-dom'
import { useCallback, useEffect, useState } from 'react'
import { api, type TitleFile } from '../api'
import { formatBytes, timeAgo } from '../format'
import { useLive } from '../useLive'
import Icon from './Icon'

const KIND_ICON: Record<TitleFile['kind'], string> = { video: 'film', subtitle: 'chat', image: 'image', nfo: 'info', other: 'folder' }

// The files in a movie's or show's folder on disk, with a Play button for
// videos. Playback is direct (no converting), so it works for formats the
// browser understands, usually MP4 and WebM, and often MKV with H.264.
export default function FilesPanel({ kind, id }: { kind: 'movie' | 'series'; id: number }) {
  const [files, setFiles] = useState<TitleFile[] | null>(null)
  const [error, setError] = useState('')
  const [playing, setPlaying] = useState<TitleFile | null>(null)
  const [viewing, setViewing] = useState<TitleFile | null>(null)

  const load = useCallback(() => {
    ;(kind === 'movie' ? api.movieFiles(id) : api.seriesFiles(id))
      .then((r) => {
        setFiles(r.files)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [kind, id])
  useEffect(load, [load])
  useLive(load, 30000)

  if (error) return <p className="error-text">{error}</p>
  if (files === null) return <div className="skeleton" style={{ height: 120 }} />
  if (files.length === 0) return <p style={{ color: 'var(--text-dim)', margin: 0 }}>No files in this title's folder yet.</p>

  const sorted = [...files].sort((a, b) => Number(b.main) - Number(a.main) || (a.kind === 'video' ? -1 : 0) - (b.kind === 'video' ? -1 : 0) || a.path.localeCompare(b.path))

  return (
    <>
      <ul className="file-list">
        {sorted.map((f) => (
          <li key={f.path} className={f.main ? 'main' : undefined}>
            <span className={`file-ico kind-${f.kind}`}>
              <Icon name={KIND_ICON[f.kind]} size={16} />
            </span>
            <div className="file-name">
              <span title={f.path}>{f.path}</span>
              <small>
                {formatBytes(f.size)} · {timeAgo(f.modified)}
                {f.main && ' · in your library'}
              </small>
            </div>
            {f.kind === 'video' && (
              <button className="btn-sm btn-with-icon" onClick={() => setPlaying(f)}>
                <Icon name="play" size={14} /> Play
              </button>
            )}
            {(f.kind === 'image' || f.kind === 'subtitle' || f.kind === 'nfo') && (
              <button className="btn-sm btn-with-icon" onClick={() => setViewing(f)}>
                <Icon name="eye" size={14} /> View
              </button>
            )}
          </li>
        ))}
      </ul>
      {playing && <Player src={api.streamUrl(kind, id, playing.path)} name={playing.path} onClose={() => setPlaying(null)} />}
      {viewing && <Viewer file={viewing} src={api.streamUrl(kind, id, viewing.path)} onClose={() => setViewing(null)} />}
    </>
  )
}

function Player({ src, name, onClose }: { src: string; name: string; onClose: () => void }) {
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return createPortal(
    <div className="modal-backdrop" onClick={onClose}>
      <div className="player" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={`Playing ${name}`}>
        <div className="player-head">
          <strong title={name}>{name.split('/').pop()}</strong>
          <button className="icon-btn" onClick={onClose} aria-label="Close">
            <Icon name="x" size={18} />
          </button>
        </div>
        {failed ? (
          <div className="player-failed">
            <Icon name="warning" size={28} />
            <p>
              Your browser cannot play this file's format. It is fine on disk: open it in your media player (Plex, Jellyfin, Kodi,
              VLC and so on).
            </p>
          </div>
        ) : (
          <video src={src} controls autoPlay onError={() => setFailed(true)} />
        )}
      </div>
    </div>,
    document.body,
  )
}

// Shows a picture, or the text of an NFO or subtitle file, in the app.
function Viewer({ file, src, onClose }: { file: TitleFile; src: string; onClose: () => void }) {
  const [text, setText] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  useEffect(() => {
    if (file.kind === 'image') return
    fetch(src, { credentials: 'include' })
      .then((r) => (r.ok ? r.text() : Promise.reject(new Error(String(r.status)))))
      .then(setText)
      .catch(() => setFailed(true))
  }, [file.kind, src])

  return createPortal(
    <div className="modal-backdrop" onClick={onClose}>
      <div className="player viewer" onClick={(e) => e.stopPropagation()} role="dialog" aria-label={`Viewing ${file.path}`}>
        <div className="player-head">
          <strong title={file.path}>{file.path.split('/').pop()}</strong>
          <button className="icon-btn" onClick={onClose} aria-label="Close">
            <Icon name="x" size={18} />
          </button>
        </div>
        {failed ? (
          <div className="player-failed">
            <Icon name="warning" size={28} />
            <p>This file cannot be shown here.</p>
          </div>
        ) : file.kind === 'image' ? (
          <img src={src} alt="" onError={() => setFailed(true)} />
        ) : text === null ? (
          <div className="skeleton" style={{ height: 200, margin: 16 }} />
        ) : (
          <pre className="viewer-text">{text}</pre>
        )}
      </div>
    </div>,
    document.body,
  )
}
