import { useEffect, useState } from 'react'
import { api } from '../api'
import CopyBox from './CopyBox'
import Icon from './Icon'

interface Library {
  label: string
  path: string
}

interface Result {
  label: string
  sameFilesystem: boolean
  supported: boolean
  error?: string
}

// Tells the person up front whether finished downloads can be moved into the
// library instantly (same filesystem) or have to be copied (different ones).
// Copying works, it just uses more disk space for a while, so this is a calm
// note and not an alarm. Used in the setup wizard and in Settings.
//
// Either pass one library folder (pathA) and the downloads folder (pathB), or
// several `libraries` to compare against the downloads folder at once.
export default function HardlinkWarning({
  pathA,
  pathB,
  libraries,
  downloads,
}: {
  pathA?: string
  pathB?: string
  libraries?: Library[]
  downloads?: string
}) {
  const list: Library[] = libraries ?? [{ label: 'library', path: pathA ?? '' }]
  const dl = downloads ?? pathB ?? ''
  const key = JSON.stringify([list, dl])
  const [results, setResults] = useState<Result[] | null>(null)

  useEffect(() => {
    const wanted = list.filter((l) => l.path.trim())
    if (!dl.trim() || wanted.length === 0) {
      setResults(null)
      return
    }
    let stale = false
    const timeout = setTimeout(async () => {
      // Debounced while the person is still typing.
      const out = await Promise.all(
        wanted.map(async (l): Promise<Result> => {
          try {
            const r = await api.filesystemCheck(l.path.trim(), dl.trim())
            return { label: l.label, ...r }
          } catch {
            return { label: l.label, sameFilesystem: false, supported: true, error: 'no answer' }
          }
        }),
      )
      if (!stale) setResults(out)
    }, 400)
    return () => {
      stale = true
      clearTimeout(timeout)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  if (!results) return null

  if (results.every((r) => !r.supported)) {
    return (
      <p className="fbox-calm">
        Can&apos;t tell if these folders share a drive here. If they do, finished downloads move instantly. If not, they&apos;re copied.
      </p>
    )
  }
  const checked = results.filter((r) => r.supported && !r.error)
  if (checked.length === 0) {
    return <p className="fbox-calm">Couldn&apos;t check the folders yet. That&apos;s normal if one doesn&apos;t exist inside the container yet.</p>
  }

  const copying = checked.filter((r) => !r.sameFilesystem)
  if (copying.length === 0) {
    return <p className="ok-line">✓ Same filesystem. Hardlinking will work.</p>
  }

  const names = copying.map((r) => r.label)
  const what = names.length === 1 ? names[0] : names.slice(0, -1).join(', ') + ' and ' + names[names.length - 1]
  return (
    <div className="callout info" role="note">
      <div className="callout-head">
        <Icon name="info" size={16} />
        <strong>Finished downloads will be copied, not moved.</strong>
      </div>
      <p>
        Your downloads folder is on a different drive or share than your {what}, so each file is copied into the library instead of moved. It works, just a bit slower.
      </p>
      <ul>
        <li>
          <strong>Usenet:</strong> the download is deleted after the copy, so you only lose a little time.
        </li>
        <li>
          <strong>Torrents:</strong> the file stays in Downloads while it seeds, so it takes twice the space until it is done seeding.
        </li>
      </ul>
      <details>
        <summary>How to make it instant</summary>
        <p>
          Keep your downloads and your library inside one shared folder, and map only that folder. For example, on a Synology map <code>/volume1/Media</code> as{' '}
          <code>/data</code>. Inside it you have Movies, tv and downloads, and Mediarium sees them as <code>/data/Movies</code>, <code>/data/tv</code> and{' '}
          <code>/data/downloads</code>. The line for your compose file:
        </p>
        <CopyBox text="- /volume1/Media:/data" label="Compose line for one shared folder" />
        <p>Then type those three paths in the boxes above.</p>
      </details>
    </div>
  )
}
