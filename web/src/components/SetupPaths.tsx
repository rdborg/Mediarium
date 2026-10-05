import { coreParent, foldersToAsk, pathInside, type Chosen, type FolderKind, type FolderValues } from '../setupHelpers'
import type { Validation } from '../useValidation'
import FolderBox from './FolderBox'
import HardlinkWarning from './HardlinkWarning'

const LIBRARY_WORD: Record<string, string> = { movies: 'movies', tv: 'TV shows', music: 'music', ebooks: 'ebooks', audiobooks: 'audiobooks' }

// The library paths step: one box for each kind of media that was chosen,
// then Downloads, then the (optional) folders for the kinds left off, in case
// they are switched on later. Every box starts from the folder the container really maps.
export default function SetupPaths({
  chosen,
  values,
  setValue,
  mapped,
  inUse,
  v,
}: {
  chosen: Chosen
  values: FolderValues
  setValue: (kind: FolderKind, value: string) => void
  mapped: FolderValues
  inUse: FolderValues
  v: Validation
}) {
  const asked = foldersToAsk(chosen)
  const libraries = asked.filter((k) => k !== 'downloads')
  const later = (['ebooks', 'audiobooks'] as const).filter((k) => !asked.includes(k))

  // A folder that already holds the others (for example /data), so a new kind
  // can be offered inside it instead of a second mapping.
  const { parent, sample } = coreParent(values)
  const inside = (kind: FolderKind) => (parent ? pathInside(parent, kind, sample) : '')

  const box = (kind: FolderKind, extra?: { optionalNote?: string; quiet?: boolean; hint?: string }) => (
    <FolderBox
      key={kind}
      kind={kind}
      value={values[kind] ?? ''}
      onChange={(x) => setValue(kind, x)}
      v={v}
      mapped={mapped[kind] ?? ''}
      inUse={inUse[kind] ?? ''}
      parent={parent}
      parentValue={inside(kind)}
      optionalNote={extra?.optionalNote}
      hint={extra?.hint}
      quiet={extra?.quiet}
    />
  )

  return (
    <>
      <div className="folder-grid">
        {libraries.map((k) => box(k))}
        {box('downloads', { hint: 'Where files are saved while they download.' })}
      </div>
      <HardlinkWarning libraries={libraries.map((k) => ({ label: LIBRARY_WORD[k], path: values[k] ?? '' }))} downloads={values.downloads ?? ''} />
      {later.length > 0 && (
        <details className="later-folders">
          <summary>Folders for {later.join(' and ')} (optional, for later)</summary>
          <p className="fbox-small">You left these off. Set their folders now if you might switch them on later.</p>
          <div className="folder-grid">{later.map((k) => box(k, { optionalNote: 'Optional', quiet: true }))}</div>
        </details>
      )}
    </>
  )
}
