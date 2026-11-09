import { createPortal } from 'react-dom'
import { useEffect, useRef, useState } from 'react'
import { api, can, isRequested, isViewer, type MusicDiscoverItem, type MusicProfile } from '../api'
import { useAuth } from '../AuthContext'
import { artistKind } from './artistKind'
import Cover from './Cover'
import Icon from './Icon'
import InfoTip from './InfoTip'
import ViewerNotice from './ViewerNotice'
import { MusicFallback } from './PosterCard'
import { useToast } from './Toast'
import { useFocusTrap } from '../useFocusTrap'
import { t, st } from '../i18n'

// What was picked on Discover: a whole artist, or one album (which may belong
// to an artist that is already in the library).
export type AddMusicTarget =
  | { kind: 'artist'; mbid: string; name: string; type?: string; country?: string; disambiguation?: string }
  // onlySong: "Add only this song", the album is added whole but only that track is wanted.
  | { kind: 'album'; item: MusicDiscoverItem; onlySong?: string }

type Choice = 'all' | 'future' | 'none' | 'album'

const TYPE_WORD: Record<string, string> = { album: t('Album'), ep: 'EP', single: t('Single') }

// The one "Add <artist>" dialog, used by Discover and by the search box at the
// top: follow every album, only the ones still to come, none of them yet, or
// just the one that was picked; which quality profile to follow; and whether to
// start looking straight away.
export default function AddMusicDialog(props: { target: AddMusicTarget; onClose: () => void; onAdded: (artistId: number, albumId?: number) => void }) {
  if (isViewer(useAuth().user)) return <ViewerNotice onClose={props.onClose} />
  return <AddMusicForm {...props} />
}

function AddMusicForm({ target, onClose, onAdded }: { target: AddMusicTarget; onClose: () => void; onAdded: (artistId: number, albumId?: number) => void }) {
  const { user } = useAuth()
  const toast = useToast()
  const item = target.kind === 'album' ? target.item : undefined
  const onlySong = target.kind === 'album' ? target.onlySong : undefined
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
        // "Only this album" goes with the add, so it also travels with a
        // request and needs no separate permission to monitor the album.
        const albumOnly = choice === 'album' && item
        const added = await api.addArtist({
          mbid: artistMbid,
          monitor: choice === 'all' ? 'all' : choice === 'future' ? 'future' : 'none',
          profileId: profileId || undefined,
          searchNow: (choice === 'all' || !!albumOnly) && willSearch,
          albumMbid: albumOnly ? item.mbid : undefined,
          albumTitle: albumOnly ? item.title : undefined,
          song: albumOnly ? onlySong : undefined,
        })
        if (isRequested(added)) {
          toast.success(st(added.message))
          onClose()
          return
        }
        artistId = added.id
        found = added.albums?.find((a) => a.mbid === item?.mbid)
      }
      let albumId: number | undefined
      if (choice === 'album' && item && existingArtist) {
        if (!found) found = (await api.getArtist(artistId)).albums?.find((a) => a.mbid === item.mbid)
        if (!found) {
          // A soundtrack or compilation the artist is on, but not one of their own albums.
          found = await api.addArtistAlbum(artistId, item.mbid, onlySong).catch(() => undefined)
        }
        if (!found) {
          toast.info(t('{name} was added, but "{album}" is not in their list of albums.', { name: artistName, album: item.title }))
        } else {
          albumId = found.id
          if (!found.monitored || onlySong) await api.setAlbumMonitored(found.id, true, onlySong)
          if (willSearch) {
            try {
              await api.searchNowAlbum(found.id)
            } catch (e) {
              toast.info(t('"{album}" was added, but the search didn\'t start: {error}', { album: item.title, error: e instanceof Error ? e.message : String(e) }))
            }
          }
        }
      }
      if (albumId === undefined && choice === 'album' && found) albumId = found.id
      toast.success(
        choice === 'album' && item && onlySong
          ? willSearch
            ? t('"{song}" added. Searching for its album now.', { song: onlySong })
            : t('"{song}" added.', { song: onlySong })
          : choice === 'album' && item
          ? willSearch
            ? t('"{album}" added. Searching for it now.', { album: item.title })
            : t('"{album}" added.', { album: item.title })
          : choice === 'all' && willSearch
            ? t('{name} added. Searching for albums now.', { name: artistName })
            : t('{name} added.', { name: artistName }),
      )
      onAdded(artistId, albumId)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const options: { id: Choice; title: string; blurb: string }[] = [
    { id: 'all', title: t('Follow all albums'), blurb: t('Every album, EP and single, and new releases as they come out.') },
    { id: 'future', title: t('Only future albums'), blurb: t('Skips albums already out and picks up new releases.') },
    ...(item ? [] : [{ id: 'none' as const, title: t('Just add the artist'), blurb: t('Nothing is followed yet. Pick the albums you want on their page.') }]),
    ...(item && !onlySong ? [{ id: 'album' as const, title: t('Only this album'), blurb: t('Just "{album}". Nothing else is downloaded.', { album: item.title }) }] : []),
    ...(item && onlySong ? [{ id: 'album' as const, title: t('Only this song'), blurb: t('"{album}" is downloaded, but only "{song}" is kept in your library.', { album: item.title, song: onlySong }) }] : []),
  ]

  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div ref={box} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={t('Add {name}', { name: artistName })}>
        <div className="modal-head">
          <div className="modal-cover">
            {item ? <Cover src={item.coverUrl} /> : <MusicFallback />}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 style={{ margin: '0 0 4px' }}>{t('Add {name}', { name: artistName })}</h2>
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
                  {TYPE_WORD[item.type] ?? t('Release')}
                </span>
                {onlySong ? `${onlySong} · ${item.title}` : item.title}
                {item.releaseDate ? ` · ${item.releaseDate.slice(0, 4)}` : ''}
              </p>
            )}
          </div>
          <button className="icon-btn" onClick={onClose} aria-label={t('Close')} disabled={busy}>
            <Icon name="x" />
          </button>
        </div>

        <div className="modal-body grid-form">
          {existingArtist ? (
            <div className="notice">
              <strong>{t('{name} is already in your library.', { name: artistName })}</strong>{' '}
              {onlySong
                ? willSearch
                  ? t('This turns on just this song and starts looking for its album.')
                  : t('This turns on just this song.')
                : willSearch
                  ? t('This turns on just this album and starts looking for it.')
                  : t('This turns on just this album.')}
            </div>
          ) : (
            <div className="choice-grid add-music-choices" role="radiogroup" aria-label={t('What to follow')}>
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
              <span className="has-tip">
                {t('Quality profile')}
                <InfoTip text={t('Decides which releases of the albums are good enough, such as FLAC or MP3. "Default" uses the profile that is set as your default.')} />
              </span>
              <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))} disabled={busy}>
                <option value={0}>{defaultProfile ? t('Default ({name})', { name: defaultProfile.name }) : t('Default')}</option>
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
            <span className="has-tip">
              {choice === 'album' ? (onlySong ? t('Start searching for this song now') : t('Start searching for this album now')) : t('Start searching for their albums now')}
              <InfoTip text={t('Looks for a release straight away. If you leave it unticked, the albums wait for the next automatic search or for you to press Search now. It is greyed out until you add an indexer.')} />
            </span>
          </label>

          {cannotSearch && (
            <div className="notice notice-warn">
              <strong>{t('No indexer yet.')}</strong> {t('It will be added and searched once you add one (Settings > Indexers & Search).')}
            </div>
          )}
          {error && <p className="error-text">{error}</p>}
        </div>

        <div className="modal-foot">
          <button onClick={onClose} disabled={busy}>
            {t('Cancel')}
          </button>
          <button className="primary btn-with-icon" onClick={submit} disabled={busy}>
            <Icon name="plus" size={16} /> {busy ? t('Adding…') : can(user, 'addDirect') || existingArtist ? (choice === 'album' ? (onlySong ? t('Add song') : t('Add album')) : t('Add artist')) : t('Request artist')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
