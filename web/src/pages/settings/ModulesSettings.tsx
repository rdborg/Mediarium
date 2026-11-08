import { useEffect, useState } from 'react'
import { api, type ModuleKey } from '../../api'
import Icon from '../../components/Icon'
import KindGlyph, { type GlyphKind } from '../../components/KindGlyph'
import Switch from '../../components/Switch'
import { useToast } from '../../components/Toast'
import { useModules } from '../../ModulesContext'
import { t, tx } from '../../i18n'
import AnimeSettingsCard from '../../components/AnimeSettingsCard'
import FeatureOffers from '../../components/FeatureOffers'
import ArchiverSettingsCard from '../../components/ArchiverSettingsCard'
import LifecycleSettingsCard from '../../components/LifecycleSettingsCard'

interface ModuleInfo {
  key: ModuleKey
  name: string
  glyph: GlyphKind
  color: string
  blurb: string
  points: string[]
}

const MODULES: ModuleInfo[] = [
  {
    key: 'movies',
    name: t('Movies'),
    glyph: 'movie',
    color: 'var(--c-movie)',
    blurb: t('Find, download and organise movies.'),
    points: [t('Discover what is popular, new and coming soon'), t('The quality you want, with upgrades if you like'), t('Named and filed for your media player'), t('Subtitles and tags such as Kids or 4K')],
  },
  {
    key: 'tv',
    name: t('TV shows'),
    glyph: 'tv',
    color: 'var(--c-tv)',
    blurb: t('Follow shows and get new episodes as they air.'),
    points: [t('New episodes picked up by themselves'), t('Whole seasons or single episodes'), t('A calendar of what airs next'), t('Filed by show and season')],
  },
  {
    key: 'anime',
    name: t('Anime'),
    glyph: 'tv',
    color: 'var(--c-anime)',
    blurb: t('Anime in a library of their own, inside TV.'),
    points: [t('Their own tab in the Library'), t('Their own folder and quality profile'), t('Files named with the absolute number'), t('The same numbering help as before')],
  },
  {
    key: 'lifecycle',
    name: t('Library lifecycle'),
    glyph: 'movie',
    color: 'var(--c-settings)',
    blurb: t('Remove what nobody watches, with a warning first.'),
    points: [t('Rules for watched and unwatched titles'), t('A Leaving soon shelf on Home'), t('One click to keep a title'), t('A dry run before anything is removed')],
  },
  {
    key: 'archiver',
    name: t('Channel archiver'),
    glyph: 'tv',
    color: 'var(--c-settings)',
    blurb: t('Keep YouTube channels with your own yt-dlp.'),
    points: [t('Videos up to 1080p, or just the audio'), t('New videos found on a timer'), t('Needs a yt-dlp that you supply'), t('Off until you accept the warning')],
  },
  {
    key: 'music',
    name: t('Music'),
    glyph: 'music',
    color: 'var(--c-music)',
    blurb: t('Keep the albums of the artists you love.'),
    points: [t('Follow artists and get their new albums'), t('FLAC or MP3, your choice'), t('Albums, singles and EPs'), t('Filed by artist and album')],
  },
  {
    key: 'ebooks',
    name: t('Ebooks'),
    glyph: 'book',
    color: 'var(--c-book)',
    blurb: t('Books for your e-reader, filed by author.'),
    points: [t('EPUB first, then AZW3, MOBI or PDF'), t('Trending books and the classics on Discover'), t('Follow authors for their new books'), t('Read them in Mediarium Books')],
  },
  {
    key: 'audiobooks',
    name: t('Audiobooks'),
    glyph: 'audiobook',
    color: 'var(--c-audiobook)',
    blurb: t('Audiobooks ready for your player, filed by author.'),
    points: [t('M4B first, then MP3 and the rest'), t('Chapters kept in order'), t('Listen in Mediarium Books, with a sleep timer'), t('Your place is saved on every device')],
  },
]

// The switchboard: which kinds of media Mediarium looks after. A module that
// is off disappears from the menu and does nothing in the background, but
// nothing is ever deleted, so switching it back on picks up where it left off.
export default function ModulesSettings() {
  const { modules, set, refresh } = useModules()
  const toast = useToast()
  const [busy, setBusy] = useState<ModuleKey | null>(null)
  const [appVersion, setAppVersion] = useState('')

  useEffect(() => {
    void refresh()
    api
      .version()
      .then((v) => setAppVersion(v.version))
      .catch(() => undefined)
  }, [refresh])

  // The anime library, the lifecycle rules and the archiver are not kinds of media, so they don't count here.
  const onCount = MODULES.filter((m) => m.key !== 'anime' && m.key !== 'lifecycle' && m.key !== 'archiver' && modules[m.key].available && modules[m.key].enabled).length

  async function change(m: ModuleInfo, next: boolean) {
    const before = modules
    // Show the change straight away; the server has the final say.
    set({ ...modules, [m.key]: { ...modules[m.key], enabled: next } })
    setBusy(m.key)
    try {
      set(await api.setModules({ [m.key]: next }))
      toast.success(next ? t('{name} is on.', { name: m.name }) : t('{name} is off. Nothing was deleted.', { name: m.name }))
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
        {tx('Pick the kinds of media you want. A switched-off one leaves the menu, and {nothing}.', { nothing: <strong>{t('nothing is deleted')}</strong> })}
      </p>
      <div className="module-grid">
        {MODULES.map((m) => {
          const st = modules[m.key]
          const soon = !st.available
          const last = st.enabled && onCount <= 1 && m.key !== 'anime' && m.key !== 'lifecycle' && m.key !== 'archiver'
          const needsTV = m.key === 'anime' && !modules.tv.enabled
          return (
            <section key={m.key} className={`module-card${st.enabled ? ' on' : ''}${soon ? ' soon' : ''}`} style={{ ['--mc' as string]: m.color }}>
              <header>
                <span className="module-ico">
                  <KindGlyph kind={m.glyph} size={40} variant="solid" />
                </span>
                <h2>{m.name}</h2>
                {!soon && <span className={`module-state ${st.enabled ? 'is-on' : 'is-off'}`}>{st.enabled ? t('On') : t('Off')}</span>}
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
                  <span className="badge module-soon">{t('Coming soon')}</span>
                ) : (
                  <Switch checked={st.enabled} onChange={(v) => void change(m, v)} disabled={last || needsTV || busy === m.key} label={st.enabled ? t('Switched on') : t('Switched off')} />
                )}
                {last && <small className="module-note">{t('At least one has to stay on.')}</small>}
                {needsTV && <small className="module-note">{t('Part of TV: switch TV on first.')}</small>}
              </footer>
            </section>
          )
        })}
      </div>
      {modules.anime.enabled && <AnimeSettingsCard />}
      {modules.lifecycle.enabled && <LifecycleSettingsCard />}
      <ArchiverSettingsCard />
      {appVersion && <FeatureOffers mode="settings" version={appVersion} />}
    </div>
  )
}
