import { NavLink } from 'react-router-dom'
import Icon, { type IconName } from './Icon'
import { t } from '../i18n'

const TABS: { to: string; label: string; icon: IconName; sec: string; end?: boolean }[] = [
  { to: '/', label: t('Home'), icon: 'grid', sec: 'dashboard', end: true },
  { to: '/discover', label: t('Discover'), icon: 'compass', sec: 'discover' },
  { to: '/library', label: t('Library'), icon: 'film', sec: 'library' },
  { to: '/queue', label: t('Activity'), icon: 'activity', sec: 'activity' },
]

// On a phone (V4 B1): the main pages along the bottom, where a thumb reaches
// them, and More for the menu with everything else. The bar is hidden on wider
// screens, where the menu on the left is always there.
export default function BottomNav({ active, menuOpen, onMenu }: { active: number; menuOpen: boolean; onMenu: () => void }) {
  return (
    <nav className="bottom-nav" aria-label={t('Main pages')}>
      {TABS.map((tab) => (
        <NavLink key={tab.to} to={tab.to} end={tab.end} style={{ ['--nc' as string]: `var(--c-${tab.sec})` }} className={({ isActive }) => (isActive && !menuOpen ? 'active' : '')}>
          <span className="bn-ico">
            <Icon name={tab.icon} size={20} />
            {tab.to === '/queue' && active > 0 && <span className="bn-count">{active > 99 ? '99+' : active}</span>}
          </span>
          <span className="bn-label">{tab.label}</span>
        </NavLink>
      ))}
      <button type="button" className={menuOpen ? 'active' : ''} onClick={onMenu} aria-controls="main-menu" aria-expanded={menuOpen}>
        <span className="bn-ico">
          <Icon name="menu" size={20} />
        </span>
        <span className="bn-label">{t('More')}</span>
      </button>
    </nav>
  )
}
