import { useBusy } from '../busy'
import Icon from './Icon'

// A banner under the top bar, on every page, while Mediarium is slow to answer
// (a page has been waiting more than ten seconds, or the app said it is busy).
// It goes away by itself once the app answers normally again.
export default function BusyNotice() {
  if (!useBusy()) return null
  return (
    <div className="import-banners">
      <div className="import-banner warn" role="status">
        <span className="import-banner-icon">
          <Icon name="warning" size={22} />
        </span>
        <div className="import-banner-body">
          <strong>Mediarium is slow to answer right now.</strong> It may be busy with a download or import.
        </div>
        <button type="button" className="btn-sm" onClick={() => window.location.reload()}>
          Try again
        </button>
      </div>
    </div>
  )
}
