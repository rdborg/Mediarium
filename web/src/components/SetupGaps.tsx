import { Link } from 'react-router-dom'
import type { SetupGap } from '../setupStatus'
import Icon from './Icon'

// The things that are still to do after setup, each with a button to the page
// that fixes it. In the wizard a click goes through onGo (it has to finish the
// wizard first); on the dashboard the buttons are plain links.
export default function SetupGaps({ gaps, onGo }: { gaps: SetupGap[]; onGo?: (to: string) => void }) {
  return (
    <ul className="setup-gaps">
      {gaps.map((g) => (
        <li key={g.key}>
          <span className="setup-gap-icon" aria-hidden="true">
            <Icon name={g.optional ? 'info' : 'warning'} size={16} />
          </span>
          <div className="setup-gap-text">
            <strong>
              {g.title}
              {g.optional && <em> · optional</em>}
            </strong>
            <span>{g.detail}</span>
          </div>
          {onGo ? (
            <button type="button" className={g.optional ? '' : 'primary'} onClick={() => onGo(g.to)}>
              {g.button}
            </button>
          ) : (
            <Link className={`btn-link${g.optional ? '' : ' primary-look'}`} to={g.to}>
              {g.button}
            </Link>
          )}
        </li>
      ))}
    </ul>
  )
}
