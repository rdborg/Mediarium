import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { api, can, isRequested, type MusicDiscoverItem, type MusicProfile } from '../api'
import { useAuth } from '../AuthContext'
import { artistKind } from './artistKind'
import Cover from './Cover'
import Icon from './Icon'
import { MusicFallback } from './PosterCard'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'

// What was picked on Discover: a whole artist, or one album (which may belong
// to an artist that is already in the library).
export type AddMusicTarget =
  | { kind: 'artist'; mbid: string; name: string; type?: string; country?: string; disambiguation?: string }
  | { kind: 'album'; item: MusicDiscoverItem }

type Choice = 'all' | 'future' | 'none' | 'album'

const TYPE_WORD: Record<string, string> = { album: 'Album', ep: 'EP', single: 'Single' }

// The one "Add <artist>" dialog, used by Discover and by the search box at the
// top: follow every album, only the ones still to come, none of them yet, or
// just the one that was picked; which quality profile to follow; and whether to
// start looking straight away.
export default function AddMusicDialog({ target, onClose, onAdded }: { target: AddMusicTarget; onClose: () => void; onAdded: (artistId: number, albumId?: number) => void }) {
  const { user } = useAuth()
  const toast = useToast()
  const item = target.kind === 'album' ? target.item : undefined
  const artistName = item ? item.artistName : target.kind === 'artist' ? target.name : ''
  const artistMbid = item ? item.artistMbid : target.kind === 'artist' ? target.mbid : ''
  // An artist that is already in the library only needs the album switched on.
  const existingArtist = item?.artistId
  const [choice, setChoice] = useState<Choice>(item ? 'album' : 'all')
  const [profiles, setProfiles] = useState<MusicProfile[]>([])
  const [profileId, setProfileId] = useState(0)
  const [searchNow, setSearchNow] = useState(true)
  const [indexers, setIndexers] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)

  useEffect(() => {
    api.musicProfiles().then(setProfiles).catch(() => undefined)
    api.dashboard().then((d) => setIndexers(d.setup.indexers)).catch(() => undefined)
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !busy && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  const defaultProfile = profiles.find((p) => p.default)
  const cannotSearch = indexers === 0
  const noSearch = choice === 'future' || choice === 'none'
  const willSearch = searchNow && !cannotSearch && !noSearch

  async function submit() {
    setBusy(true)
    setError('')
    try {
      let artistId = existingArtist
      let found = undefined as { id: number; monitored: boolean } | undefined
      if (!artistId) {
        const added = await api.addArtist({
          mbid: artistMbid,
          monitor: choice === 'all' ? 'all' : choice === 'future' ? 'future' : 'none',
          profileId: profileId || undefined,
          searchNow: choice === 'all' && willSearch,
        })
        if (isRequested(added)) {
          toast.success(added.message)
          onClose()
          return
        }
        artistId = added.id
        found = added.albums?.find((a) => a.mbid === item?.mbid)
      }
      let albumId: number | undefined
      if (choice === 'album' && item) {
        if (!found) found = (await api.getArtist(artistId)).albums?.find((a) => a.mbid === item.mbid)
        if (!found) {
          toast.info(`${artistName} was added, but "${item.title}" is not in their list of albums.`)
        } else {
          albumId = found.id
          if (!found.monitored) await api.setAlbumMonitored(found.id, true)
          if (willSearch) {
            try {
              await api.searchNowAlbum(found.id)
            } catch (e) {
              toast.info(`"${item.title}" was added, but the search didn't start: ${e instanceof Error ? e.message : String(e)}`)
            }
          }
        }
      }
      toast.success(
        choice === 'album' && item
          ? `"${item.title}" added.${willSearch ? ' Searching for it now.' : ''}`
          : `${artistName} added.${choice === 'all' && willSearch ? ' Searching for albums now.' : ''}`,
      )
      onAdded(artistId, albumId)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const options: { id: Choice; title: string; blurb: string }[] = [
    { id: 'all', title: 'Follow all albums', blurb: 'Every album, EP and single, and new releases as they come out.' },
    { id: 'future', title: 'Only future albums', blurb: 'Skips albums already out and picks up new releases.' },
    ...(item ? [] : [{ id: 'none' as const, title: 'Just add the artist', blurb: 'Nothing is followed yet. Pick the albums you want on their page.' }]),
    ...(item ? [{ id: 'album' as const, title: 'Only this album', blurb: `Just "${item.title}". Nothing else is downloaded.` }] : []),
  ]

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={`Add ${artistName}`}>
        <div className="modal-head">
          <div className="modal-cover">
            {item ? <Cover src={item.coverUrl} /> : <MusicFallback />}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>Add {artistName}</h2>
            {!item && target.kind === 'artist' && (
              <>
                <span className="badge">{artistKind(target.type)}</span>
                {target.country && (
                  <span className="badge" style={{ marginLeft: 6 }}>
                    {target.country}
                  </span>
                )}
                {target.disambiguation && <p style={{ margin: '6px 0 0', color: 'var(--text-dim)', fontSize: '0.88rem' }}>{target.disambiguation}</p>}
              </>
            )}
            {item && (
              <p style={{ margin: 0, color: 'var(--text-dim)' }}>
                <span className="badge" style={{ marginRight: 6 }}>
                  {TYPE_WORD[item.type] ?? 'Release'}
                </span>
                {item.title}
                {item.releaseDate ? ` · ${item.releaseDate.slice(0, 4)}` : ''}
              </p>
            )}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label="Close" disabled={busy}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          {existingArtist ? (
            <div className="notice">
              <strong>{artistName} is already in your library.</strong> This turns on just this album{willSearch ? ' and starts looking for it' : ''}.
            </div>
          ) : (
            <div className="choice-grid add-music-choices" role="radiogroup" aria-label="What to follow">
              {options.map((o) => (
                <button key={o.id} type="button" role="radio" aria-checked={choice === o.id} className={`choice-card${choice === o.id ? ' active' : ''}`} onClick={() => setChoice(o.id)} disabled={busy}>
                  <span className="choice-dot" aria-hidden="true" />
                  <strong>{o.title}</strong>
                  <small>{o.blurb}</small>
                </button>
              ))}
            </div>
          )}

          {!existingArtist && (
            <label>
              Quality profile
              <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))} disabled={busy}>
                <option value={0}>Default{defaultProfile ? ` (${defaultProfile.name})` : ''}</option>
                {profiles.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          )}

          <label style={{ flexDirection: 'row', alignItems: 'center', gap: 8, color: 'var(--text)' }}>
            <input type="checkbox" checked={searchNow && !cannotSearch && !noSearch} onChange={(e) => setSearchNow(e.target.checked)} disabled={cannotSearch || noSearch || busy} />
            {choice === 'album' ? 'Start searching for this album now' : 'Start searching for their albums now'}
          </label>

          {cannotSearch && (
            <div className="notice notice-warn">
              <strong>No indexer yet.</strong> It will be added and searched once you add one (Settings &gt; Indexers &amp; Search).
            </div>
          )}
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="primary btn-with-icon" onClick={submit} disabled={busy}>
            <Icon name="plus" size={16} /> {busy ? 'Adding…' : can(user, 'addDirect') || existingArtist ? (choice === 'album' ? 'Add album' : 'Add artist') : 'Request artist'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
