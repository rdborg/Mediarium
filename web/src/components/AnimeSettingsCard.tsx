import { useEffect, useState } from 'react'
import { api, type AnimeSettings, type QualityProfile } from '../api'
import { useToast } from './Toast'
import TipLabel from './TipLabel'
import { sortProfiles } from './qualityBlurb'
import { t } from '../i18n'

// Settings > Media types, under the cards while the anime library is on: the
// folder new anime shows go to, their default quality profile and their file
// names. Each is optional; left empty, anime use what every show uses.
export default function AnimeSettingsCard() {
  const toast = useToast()
  const [saved, setSaved] = useState<AnimeSettings | null>(null)
  const [path, setPath] = useState('')
  const [moviesPath, setMoviesPath] = useState('')
  const [format, setFormat] = useState('')
  const [profileId, setProfileId] = useState(0)
  const [profiles, setProfiles] = useState<QualityProfile[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getAnimeSettings()
      .then((s) => {
        setSaved(s)
        setPath(s.path)
        setMoviesPath(s.moviesPath)
        setFormat(s.episodeFormat)
        setProfileId(s.profileId)
      })
      .catch(() => undefined)
    api
      .listProfiles()
      .then((r) => setProfiles(sortProfiles(r.profiles)))
      .catch(() => undefined)
  }, [])

  if (!saved) return null
  const changed = path.trim() !== saved.path || moviesPath.trim() !== saved.moviesPath || format.trim() !== saved.episodeFormat || profileId !== saved.profileId

  async function save() {
    setBusy(true)
    setError('')
    try {
      const s = await api.putAnimeSettings({ path: path.trim(), moviesPath: moviesPath.trim(), episodeFormat: format.trim(), profileId })
      setSaved(s)
      setPath(s.path)
      setMoviesPath(s.moviesPath)
      setFormat(s.episodeFormat)
      setProfileId(s.profileId)
      toast.success(t('Anime library settings saved.'))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card anime-settings" style={{ ['--mc' as string]: 'var(--c-anime)' }}>
      <h2>{t('The anime library')}</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t('Anime shows have their own tab in the Library. These apply to anime shows added from now on; one already in your library keeps its folder and profile until you change them on its page.')}
      </p>
      <div className="grid-form">
        <label>
          <TipLabel text={t('New anime shows go here instead of the TV folder. It has to be a folder of its own, not inside the TV folder. Leave it empty to keep anime in the TV folder.')}>{t('Anime folder')}</TipLabel>
          <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/anime" spellCheck={false} autoComplete="off" disabled={busy} />
        </label>
        <label>
          <TipLabel text={t('New anime films (animated and made in Japanese) go here instead of the movie folder, unless you pick another folder when you add one. Leave it empty to keep them with your other movies.')}>
            {t('Anime films folder')}
          </TipLabel>
          <input value={moviesPath} onChange={(e) => setMoviesPath(e.target.value)} placeholder="/anime-films" spellCheck={false} autoComplete="off" disabled={busy} />
        </label>
        <label>
          <TipLabel
            text={t('The quality profile an anime show gets when it is added without one. To prefer some fansub groups, open that profile and press Add anime groups under its preferred terms.')}
            learnMore="quality#anime-release-groups"
          >
            {t('Quality profile for anime')}
          </TipLabel>
          <select value={profileId} onChange={(e) => setProfileId(Number(e.target.value))} disabled={busy}>
            <option value={0}>{t('The same as for other shows')}</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          <TipLabel text={t('How anime episode files are named. {Absolute:000} is the number counted from the very first episode, the way anime releases are named. Leave it empty to name anime like other shows.')}>{t('Anime episode names')}</TipLabel>
          <input value={format} onChange={(e) => setFormat(e.target.value)} placeholder={t('The same as for other shows')} spellCheck={false} autoComplete="off" disabled={busy} />
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <button className="primary" disabled={busy || !changed} onClick={() => void save()}>
            {busy ? t('Saving…') : t('Save')}
          </button>
          {format.trim() !== saved.suggestedFormat && (
            <button disabled={busy} onClick={() => setFormat(saved.suggestedFormat)}>
              {t('Use names with the absolute number')}
            </button>
          )}
        </div>
        {error && <p className="error-text">{error}</p>}
      </div>
    </section>
  )
}
