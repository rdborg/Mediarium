import { Link } from 'react-router-dom'
import { isAdmin } from '../../api'
import { useAuth } from '../../AuthContext'
import Icon from '../../components/Icon'
import { t, tx } from '../../i18n'
import { settingsGroupsFor } from '../../settingsNav'

// What shows at /settings: every group with its pages and what each one is
// for, side by side, so a setting can be found by topic without opening
// anything. The search (Ctrl+K) is pointed out underneath.
export default function SettingsOverview() {
  const { user } = useAuth()
  const groups = settingsGroupsFor(isAdmin(user), user?.role === 'viewer')
  return (
    <div className="settings-overview">
      <p className="settings-blurb">{t('Everything you can change, grouped in the order most people set it up.')}</p>
      <div className="settings-stack so-grid">
        {groups.map((g) => (
          <section key={g.key} className="card so-group" style={{ ['--sc' as string]: g.color }}>
            <h2>
              <span className="so-ico">
                <Icon name={g.icon} size={15} />
              </span>
              {g.label}
            </h2>
            <p className="so-blurb">{g.blurb}</p>
            <ul className="so-pages">
              {g.pages.map((p) => (
                <li key={p.to}>
                  <Link to={p.to} title={p.blurb}>
                    <span className="so-name">{p.label}</span>
                    <span className="so-desc">{p.blurb}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
      <p className="so-hint">{tx("Can't find a setting? Press {keys} or use the search bar.", { keys: <kbd>Ctrl+K</kbd> })}</p>
    </div>
  )
}
