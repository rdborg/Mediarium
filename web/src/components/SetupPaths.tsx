import { coreParent, foldersToAsk, pathInside, type Chosen, type FolderKind, type FolderValues } from '../setupHelpers'
import type { Validation } from '../useValidation'
import FolderBox from './FolderBox'
import HardlinkWarning from './HardlinkWarning'

const LIBRARY_WORD: Record<string, string> = { movies: 'movies', tv: 'TV shows', music: 'music' }

// The library paths step: one box for each kind of media that was chosen,
// then Downloads, then the (optional) folders for things that are coming
// later. Every box starts from the folder the container really maps.
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
      <details className="later-folders">
        <summary>Folders for ebooks and audiobooks (optional, for later)</summary>
        <p className="fbox-small">
          Not available yet. Set their folders now and Mediarium keeps them for when they arrive.
        </p>
        <div className="folder-grid">
          {box('ebooks', { optionalNote: 'Optional', quiet: true })}
          {box('audiobooks', { optionalNote: 'Optional', quiet: true })}
        </div>
      </details>
    </>
  )
}
