import { useEffect, useState } from 'react'
import { api, type ModuleKey } from '../../api'
import Icon, { type IconName } from '../../components/Icon'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import { useModules } from '../../ModulesContext'

interface ModuleInfo {
  key: ModuleKey
  name: string
  icon: IconName
  blurb: string
  points: string[]
}

const MODULES: ModuleInfo[] = [
  {
    key: 'movies',
    name: 'Movies',
    icon: 'film',
    blurb: 'Find, download and organise movies.',
    points: ['Discover what is popular, new and coming soon', 'The quality you want, with upgrades if you like', 'Named and filed for your media player', 'Subtitles and tags such as Kids or 4K'],
  },
  {
    key: 'tv',
    name: 'TV shows',
    icon: 'tv',
    blurb: 'Follow shows and get new episodes as they air.',
    points: ['New episodes picked up by themselves', 'Whole seasons or single episodes', 'A calendar of what airs next', 'Filed by show and season'],
  },
  {
    key: 'music',
    name: 'Music',
    icon: 'music',
    blurb: 'Keep the albums of the artists you love.',
    points: ['Follow artists and get their new albums', 'FLAC or MP3, your choice', 'Albums, singles and EPs', 'Filed by artist and album'],
  },
  {
    key: 'ebooks',
    name: 'Ebooks',
    icon: 'book',
    blurb: 'Books for your e-reader, filed by author.',
    points: ['EPUB first, then AZW3, MOBI or PDF', 'Trending books and the classics on Discover', 'Follow authors for their new books', 'Read them in Mediarium Books'],
  },
  {
    key: 'audiobooks',
    name: 'Audiobooks',
    icon: 'headphones',
    blurb: 'Audiobooks ready for your player, filed by author.',
    points: ['M4B first, then MP3 and the rest', 'Chapters kept in order', 'Listen in Mediarium Books, with a sleep timer', 'Your place is saved on every device'],
  },
]

// The switchboard: which kinds of media Mediarium looks after. A module that
// is off disappears from the menu and does nothing in the background, but
// nothing is ever deleted, so switching it back on picks up where it left off.
export default function ModulesSettings() {
  const { modules, set, refresh } = useModules()
  const toast = useToast()
  const [busy, setBusy] = useState<ModuleKey | null>(null)

  useEffect(() => {
    void refresh()
  }, [refresh])

  const onCount = MODULES.filter((m) => modules[m.key].available && modules[m.key].enabled).length

  async function change(m: ModuleInfo, next: boolean) {
    const before = modules
    // Show the change straight away; the server has the final say.
    set({ ...modules, [m.key]: { ...modules[m.key], enabled: next } })
    setBusy(m.key)
    try {
      set(await api.setModules({ [m.key]: next }))
      toast.success(next ? `${m.name} is on.` : `${m.name} is off. Nothing was deleted.`)
    } catch (e) {
      set(before)
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="module-page">
      <p className="module-intro">
        Pick the kinds of media you want. A switched-off one leaves the menu, and <strong>nothing is deleted</strong>.
      </p>
      <div className="module-grid">
        {MODULES.map((m) => {
          const st = modules[m.key]
          const soon = !st.available
          const last = st.enabled && onCount <= 1
          return (
            <section key={m.key} className={`module-card${st.enabled ? ' on' : ''}${soon ? ' soon' : ''}`}>
              <header>
                <span className="module-ico">
                  <Icon name={m.icon} size={26} />
                </span>
                <h2>{m.name}</h2>
                {!soon && <span className={`module-state ${st.enabled ? 'is-on' : 'is-off'}`}>{st.enabled ? 'On' : 'Off'}</span>}
              </header>
              <p className="module-blurb">{m.blurb}</p>
              <ul className="module-points">
                {m.points.map((pt) => (
                  <li key={pt}>
                    <Icon name="check" size={14} /> {pt}
                  </li>
                ))}
              </ul>

              <footer>
                {soon ? (
                  <span className="badge module-soon">Coming soon</span>
                ) : (
                  <Switch checked={st.enabled} onChange={(v) => void change(m, v)} disabled={last || busy === m.key} label={st.enabled ? 'Switched on' : 'Switched off'} />
                )}
                {last && <small className="module-note">At least one has to stay on.</small>}
              </footer>
            </section>
          )
        })}
      </div>
    </div>
  )
}
