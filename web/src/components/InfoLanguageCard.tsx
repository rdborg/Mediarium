import { useEffect, useState } from 'react'
import { api } from '../api'
import { currentLanguage, locale, t, type Lang } from '../i18n'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'

// The TMDB language that goes with each screen language, for the "use the same
// as the screen" shortcut.
const FOR_SCREEN: Record<Lang, string> = { en: 'en-US', es: 'es-ES', de: 'de-DE', fr: 'fr-FR', it: 'it-IT', nl: 'nl-NL', pl: 'pl-PL', pt: 'pt-BR', 'pt-PT': 'pt-PT', ru: 'ru-RU', uk: 'uk-UA', 'zh-CN': 'zh-CN' }

// A country's name in the screen language ("IN" gives "India"), or the code
// itself where the browser doesn't know it.
function countryName(code: string): string {
  try {
    return new Intl.DisplayNames(locale() ?? 'en', { type: 'region' }).of(code) ?? code
  } catch {
    return code
  }
}

// The language titles, descriptions, genres and posters are read in. It is its
// own choice, apart from the screen language: someone can read Mediarium in
// English and still want film titles in Portuguese. New movie and show folders
// and file names use the same title. The titles searched for stay in English.
export default function InfoLanguageCard({ headingLevel = 2 }: { headingLevel?: 2 | 3 }) {
  const toast = useToast()
  const [value, setValue] = useState('')
  const [options, setOptions] = useState<{ tag: string; name: string }[]>([])
  const [country, setCountry] = useState('')
  const [countries, setCountries] = useState<string[]>([])
  const [folders, setFolders] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setValue(s.infoLanguage ?? 'en-US')
        setOptions(s.infoLanguages ?? [])
        setCountry(s.releaseCountry ?? '')
        setCountries(s.releaseCountries ?? [])
        setFolders(s.foldersInInfoLanguage ?? false)
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  async function choose(tag: string) {
    setBusy(true)
    try {
      await api.putSettings({ infoLanguage: tag })
      setValue(tag)
      const name = options.find((o) => o.tag === tag)?.name ?? tag
      toast.success(t('Saved. Discover, search and title pages now use {language}.', { language: name }))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function chooseFolders(on: boolean) {
    setBusy(true)
    try {
      await api.putSettings({ foldersInInfoLanguage: on })
      setFolders(on)
      toast.success(on ? t('Saved. New movie and show folders use the title in this language.') : t('Saved. New movie and show folders use the English title.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function chooseCountry(code: string) {
    setBusy(true)
    try {
      await api.putSettings({ releaseCountry: code })
      setCountry(code)
      toast.success(code ? t('Saved. Release dates now use {country}.', { country: countryName(code) }) : t('Saved. Release dates follow the info language.'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const sortedCountries = [...countries].sort((a, b) => countryName(a).localeCompare(countryName(b), locale()))
  const suggested = FOR_SCREEN[currentLanguage()]
  const suggestedName = options.find((o) => o.tag === suggested)?.name
  const Heading = headingLevel === 2 ? 'h2' : 'h3'
  return (
    <section className="card info-language">
      <Heading>{t('Language for movie and show info')}</Heading>
      <p>{t('Titles, descriptions, genres and posters come from TMDB in this language. Where TMDB has no translation you see the English text.')}</p>
      <label className="field">
        {t('Show titles and descriptions in')}
        <select value={value} disabled={busy || options.length === 0} onChange={(e) => void choose(e.target.value)}>
          {options.map((o) => (
            <option key={o.tag} value={o.tag}>
              {o.name}
            </option>
          ))}
        </select>
      </label>
      <label className="field">
        {t('Country for release dates')}
        <select value={country} disabled={busy || countries.length === 0} onChange={(e) => void chooseCountry(e.target.value)}>
          <option value="">{t('Same as the info language')}</option>
          {sortedCountries.map((code) => (
            <option key={code} value={code}>
              {countryName(code)}
            </option>
          ))}
        </select>
      </label>
      <p className="field-hint">{t('The cinema and digital dates on a movie page and in the calendar are the ones for this country. Where it has none, the US date is shown with (US).')}</p>
      {suggestedName && suggested !== value && (
        <button type="button" className="link-btn" disabled={busy} onClick={() => void choose(suggested)}>
          <Icon name="globe" size={14} /> {t('Use {language}, like the screen', { language: suggestedName })}
        </button>
      )}
      <Switch
        checked={folders}
        onChange={(on) => void chooseFolders(on)}
        disabled={busy}
        label={t('Name new movie and show folders in this language')}
        description={t("A new movie or show gets a folder and file named with the title shown here, like Der Pate (1972). A title that already has files keeps its folder, and indexer searches stay in English. Plex and Jellyfin match by folder name, so check your library after switching it on.")}
      />
      {error && <p className="error-text">{error}</p>}
    </section>
  )
}
