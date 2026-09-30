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
  color: string
  blurb: string
}

const MODULES: ModuleInfo[] = [
  { key: 'movies', name: 'Movies', icon: 'film', color: 'var(--c-movie)', blurb: 'Find, download and organise movies, like Radarr.' },
  { key: 'tv', name: 'TV shows', icon: 'tv', color: 'var(--c-tv)', blurb: 'Follow shows and get new episodes as they air, like Sonarr.' },
  { key: 'music', name: 'Music', icon: 'music', color: 'var(--c-music)', blurb: 'Keep the albums of the artists you love, in FLAC or MP3, like Lidarr.' },
  { key: 'audiobooks', name: 'Audiobooks', icon: 'headphones', color: 'var(--text-dim)', blurb: 'Audiobooks, named and filed neatly.' },
  { key: 'ebooks', name: 'Ebooks', icon: 'book', color: 'var(--text-dim)', blurb: 'Ebooks and comics for your e-reader.' },
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
            <section key={m.key} className={`module-card${st.enabled ? ' on' : ''}${soon ? ' soon' : ''}`} style={{ ['--mc' as string]: m.color }}>
              <header>
                <span className="module-ico">
                  <Icon name={m.icon} size={22} />
                </span>
                <h2>{m.name}</h2>
                {!soon && <span className={`module-state ${st.enabled ? 'is-on' : 'is-off'}`}>{st.enabled ? 'On' : 'Off'}</span>}
              </header>
              <p className="module-blurb">{m.blurb}</p>

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
