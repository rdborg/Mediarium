import type { SubtitleQuota } from '../api'
import Icon from './Icon'

// Says how many subtitle downloads are left today and warns, in plain words,
// when the subtitles waiting to be fetched will not fit in one day's allowance.
export default function QuotaNote({ quota }: { quota: SubtitleQuota | null }) {
  if (!quota || !quota.hasKey) return null
  const pct = quota.limit > 0 ? Math.min(100, Math.round((quota.used / quota.limit) * 100)) : 0
  const warn = quota.exceeded || (quota.missingFiles > quota.remaining && quota.missingFiles > 0)
  return (
    <div className={`quota-note${warn ? ' warn' : ''}`}>
      <div className="quota-head">
        <Icon name={warn ? 'warning' : 'info'} size={16} />
        <strong>
          {quota.remaining} of {quota.limit} subtitle downloads left today
        </strong>
        {quota.resetsAt && quota.used > 0 && <small>refills {new Date(quota.resetsAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</small>}
      </div>
      <div className={`bar${warn ? ' warn' : ''}`}>
        <span style={{ width: `${Math.max(3, pct)}%` }} />
      </div>
      {quota.message && <p>{quota.message}</p>}
    </div>
  )
}
