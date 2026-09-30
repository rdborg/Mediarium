import { useEffect, useState } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { api } from '../api'

// Downloads and the dashboard point at an album by its number. Albums live on
// their artist's page, so this looks the artist up and goes there, with that
// album opened.
export default function MusicAlbumRedirect() {
  const { id } = useParams<{ id: string }>()
  const [to, setTo] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    api
      .getAlbum(Number(id))
      .then((a) => setTo(`/music/artist/${a.artistId}#album-${a.id}`))
      .catch(() => setFailed(true))
  }, [id])
  if (failed) return <Navigate to="/library?kind=music" replace />
  if (to) return <Navigate to={to} replace />
  return <div className="skeleton" style={{ height: 200, borderRadius: 22 }} />
}
