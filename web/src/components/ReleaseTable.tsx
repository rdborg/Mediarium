import type { SearchResult } from '../api'
import { formatBytes } from '../format'

// Interactive-search results: every release with quality info, badges for
// packs / blocklisted / "automation would skip this" (with the reason on
// hover), and a Grab button — grabbing a rejected release by hand is always
// allowed.
export default function ReleaseTable({
  results,
  grabbing,
  onGrab,
  showPackBadge = false,
}: {
  results: SearchResult[]
  grabbing: string | null
  onGrab: (r: SearchResult) => void
  showPackBadge?: boolean
}) {
  return (
    <table>
      <thead>
        <tr>
          <th>Release</th>
          <th>Quality</th>
          <th>Indexer</th>
          <th>Size</th>
          <th>Seeders</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {results.map((r) => (
          <tr key={r.downloadUrl} style={r.rejections?.length || r.blocklisted ? { opacity: 0.75 } : undefined}>
            <td style={{ wordBreak: 'break-all' }}>
              {r.title}
              {showPackBadge && (r.episodes?.length ?? 0) === 0 && (
                <span className="badge" style={{ marginLeft: 8 }}>
                  season pack
                </span>
              )}
              {r.blocklisted && (
                <span className="badge failed" style={{ marginLeft: 8 }} title="This release failed before">
                  blocklisted
                </span>
              )}
              {r.acceptedBy?.fallback && (
                <span className="badge fallback-badge" style={{ marginLeft: 8 }} title={`Only used if nothing is found at the title's own quality: accepted by the "${r.acceptedBy.profileName}" fallback`}>
                  fallback: {r.acceptedBy.profileName}
                </span>
              )}
              {r.rejections && r.rejections.length > 0 && !r.acceptedBy?.fallback && (
                <span className="badge conflict" style={{ marginLeft: 8 }} title={r.rejections.join('\n')}>
                  {r.rejections[0]}
                </span>
              )}
            </td>
            <td>{[r.resolution, r.source, r.codec].filter(Boolean).join(' · ') || '—'}</td>
            <td>
              <span className="badge" title={r.protocol}>
                {r.protocol === 'torrent' ? '⇅' : '⇩'} {r.indexerName}
              </span>
            </td>
            <td>{formatBytes(r.sizeBytes)}</td>
            <td>{r.protocol === 'torrent' ? `${r.seeders ?? 0} / ${r.peers ?? 0}` : '—'}</td>
            <td>
              <button className="primary" disabled={grabbing === r.downloadUrl} onClick={() => onGrab(r)}>
                {grabbing === r.downloadUrl ? 'Grabbing…' : 'Grab'}
              </button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
