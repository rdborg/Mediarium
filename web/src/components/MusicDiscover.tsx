import { useNavigate } from 'react-router-dom'
import AddMusicDialog, { type AddMusicTarget } from './AddMusicDialog'
import MusicRails, { MusicBrowseGrid, type MusicBrowseFilters } from './MusicRails'

// The music part of Discover: the popular, new and coming-soon rails, or,
// once a filter is picked, one browsable list of everything that matches.
// It sits in the same page and under the same filter row as movies and shows.
export function MusicBody({ filters, filtering, onAdd, onClear }: { filters: MusicBrowseFilters; filtering: boolean; onAdd: (t: AddMusicTarget) => void; onClear?: () => void }) {
  return filtering ? <MusicBrowseGrid filters={filters} onAdd={onAdd} onClear={onClear} /> : <MusicRails type="all" genre="" onAdd={onAdd} />
}

// The add dialog the music cards open, and where it goes once something was
// added: the artist's page.
export function MusicAddDialog({ target, onClose }: { target: AddMusicTarget | null; onClose: () => void }) {
  const navigate = useNavigate()
  if (!target) return null
  return (
    <AddMusicDialog
      target={target}
      onClose={onClose}
      onAdded={(artistId, albumId) => {
        onClose()
        navigate(`/music/artist/${artistId}${albumId ? `#album-${albumId}` : ''}`)
      }}
    />
  )
}
