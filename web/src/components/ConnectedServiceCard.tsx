import type { ReactNode } from 'react'
import Icon, { type IconName } from './Icon'

// A service that already ships with Mediarium: what it is, that it is
// connected, what its shared limits are, and where to get a personal key if
// the limits ever get in the way.
export default function ConnectedServiceCard({
  icon,
  title,
  does,
  limits,
  own,
}: {
  icon: IconName
  title: string
  does: ReactNode
  limits: ReactNode
  own: ReactNode
}) {
  return (
    <section className="card service-card is-included connected-card">
      <h2>
        <span className="cc-ico">
          <Icon name={icon} size={18} />
        </span>
        {title} <span className="req-badge included">Connected</span>
      </h2>
      <p className="cc-does">{does}</p>
      <div className="cc-limits">
        <strong>Limits of the shared key</strong>
        <p>{limits}</p>
      </div>
      <p className="cc-own">
        <Icon name="key" size={14} /> {own}
      </p>
    </section>
  )
}
