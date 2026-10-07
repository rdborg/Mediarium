import { NavLink } from 'react-router-dom'
import Icon from '../components/Icon'
import Calendar from './Calendar'
import Wanted from './Wanted'

// Upcoming: what is coming (the calendar) and what is still missing or could
// be better (wanted), as two tabs of one page.
export default function Upcoming({ tab }: { tab: 'calendar' | 'wanted' }) {
  return (
    <div>
      <nav className="page-tabs" aria-label="Upcoming">
        <NavLink to="/calendar" className={({ isActive }) => (isActive ? 'active' : '')}>
          <Icon name="calendar" size={15} /> Calendar
        </NavLink>
        <NavLink to="/wanted" className={({ isActive }) => (isActive ? 'active' : '')}>
          <Icon name="bookmark" size={15} /> Wanted
        </NavLink>
      </nav>
      {tab === 'calendar' ? <Calendar /> : <Wanted />}
    </div>
  )
}
