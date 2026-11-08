import { useCallback, useEffect, useState } from 'react'
import { api, type Settings as SettingsData } from '../../api'
import GoogleBooksCard from '../../components/GoogleBooksCard'
import HardcoverCard from '../../components/HardcoverCard'
import TheTVDBCard from '../../components/TheTVDBCard'
import OMDbCard from '../../components/OMDbCard'
import InfoLanguageCard from '../../components/InfoLanguageCard'
import ServiceKeyCard from '../../components/ServiceKeyCard'
import { TMDB_COPY } from '../../components/serviceCopy'
import Switch from '../../components/Switch'
import { t } from '../../i18n'
import { useAutosaveSetting } from '../../useAutosave'
import { useModules } from '../../ModulesContext'
import TipLabel from '../../components/TipLabel'

// The trailer behind movie and show pages, for everyone. A browser can still choose for itself with the button on the page.
function TrailerCard() {
  const { refresh } = useModules()
  const trailer = useAutosaveSetting<boolean>(
    (s) => s.trailerBackground ?? true,
    (v) => ({ trailerBackground: v }),
    (saved) => (saved ? t('Saved: the trailer plays behind movie and show pages.') : t('Saved: the trailer stays off unless a browser switches it on.')),
    true,
  )
  const sound = useAutosaveSetting<boolean>(
    (s) => s.trailerSound ?? false,
    (v) => ({ trailerSound: v }),
    (saved) => (saved ? t('Saved: the trailer starts with its sound on.') : t('Saved: the trailer starts muted.')),
    false,
  )
  return (
    <section className="card">
      <h2>{t('Trailers')}</h2>
      <Switch
        checked={trailer.value}
        onChange={(v) => void trailer.change(v).then(refresh)}
        disabled={!trailer.loaded || trailer.saving}
        label={t('Play the trailer behind movie and show pages')}
        description={t('The first trailer plays muted in place of the still picture on a title page, and the picture comes back when it ends. The video comes from YouTube, so YouTube sees every title page that is opened with this on; switch it off if you would rather not. Each browser can still switch it off or on for itself with the button on the page.')}
        showState
      />
      <Switch
        checked={sound.value}
        onChange={(v) => void sound.change(v).then(refresh)}
        disabled={!sound.loaded || sound.saving || !trailer.value}
        label={t('Start the trailer with its sound on')}
        description={t('Browsers only let a page play sound after you have clicked something on it, so the trailer may still start muted when a page is opened straight from a link. The small sound button on the page changes it for that browser.')}
        showState
      />
    </section>
  )
}

// Where posters and backdrops are loaded from: this server (the default) or TMDB's picture host directly.
function ArtworkCard() {
  const { refresh } = useModules()
  const art = useAutosaveSetting<boolean>(
    (s) => s.artworkProxy ?? true,
    (v) => ({ artworkProxy: v }),
    (saved) => (saved ? t('Saved: artwork loads through Mediarium.') : t('Saved: browsers load artwork straight from TMDB.')),
    true,
  )
  return (
    <section className="card">
      <h2>{t('Artwork from TMDB')}</h2>
      <Switch
        checked={art.value}
        onChange={(v) => void art.change(v).then(refresh)}
        disabled={!art.loaded || art.saving}
        label={t('Load artwork through Mediarium')}
        description={t('Mediarium gets each poster, backdrop and picture of a person from TMDB once, makes it the size the page needs and keeps a copy. Your browsers then never contact TMDB, and phones load small pictures. Switched off, every browser loads artwork from TMDB itself. Pages that are open change when they load again.')}
        showState
      />
    </section>
  )
}

// Anime and scene numbering: where the other numbering of a show comes from.
function NumberingCard() {
  const sw = useAutosaveSetting<boolean>(
    (s) => s.numberingLookup ?? true,
    (v) => ({ numberingLookup: v }),
    (saved) => (saved ? t('Saved: anime numbering is looked up.') : t('Saved: anime numbering is not looked up on its own.')),
    true,
  )
  return (
    <section className="card">
      <h2><TipLabel help="library#scene-and-anime-numbering">{t('Anime numbering')}</TipLabel></h2>
      <Switch
        checked={sw.value}
        onChange={(v) => void sw.change(v)}
        disabled={!sw.loaded || sw.saving}
        label={t('Look up how anime and scene releases number their episodes')}
        description={t("When you add an anime, Mediarium asks TheXEM, the anime lists on GitHub and AniList how its releases are numbered and what other names it has, so releases named in romaji or by the part of a season are found. TheXEM is asked with the show's TheTVDB id and AniList with the AniList ids of its seasons; GitHub only hands out a list. Shows that are not anime are left out unless you press Refresh mapping on the show's page. Switching this off keeps what was found and stops new lookups; the button still works.")}
        showState
      />
    </section>
  )
}

// Air times: TVmaze knows the time an episode airs, which TMDB does not.
function AirTimesCard() {
  const sw = useAutosaveSetting<boolean>(
    (s) => s.tvmazeAirTimes ?? true,
    (v) => ({ tvmazeAirTimes: v }),
    (saved) => (saved ? t('Saved: air times are looked up on TVmaze.') : t('Saved: air times are not looked up. TVmaze is not contacted.')),
    true,
  )
  return (
    <section className="card">
      <h2>{t('Air times')}</h2>
      <Switch
        checked={sw.value}
        onChange={(v) => void sw.change(v)}
        disabled={!sw.loaded || sw.saving}
        label={t('Look up air times on TVmaze')}
        description={t("TMDB only gives the day an episode airs. TVmaze also has the time, so the calendar can show each episode on the day and hour it airs in your time zone. Only the show's TheTVDB or IMDb id, or its name and year, is sent, and nothing about you. Switching this off means TVmaze is never contacted; the times already found stay until the show's dates change.")}
        showState
      />
    </section>
  )
}

export default function MetadataSettings() {
  const [s, setS] = useState<SettingsData | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    api
      .getSettings()
      .then((data) => {
        setS(data)
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  if (!s) {
    return error ? (
      <div>
        <p className="error-text">{error}</p>
        <button onClick={load}>{t('Try again')}</button>
      </div>
    ) : (
      <p>{t('Loading…')}</p>
    )
  }
  return (
    <div className="settings-stack">
      <ServiceKeyCard service="tmdb" {...TMDB_COPY} builtIn={!!s.tmdbKeyBuiltIn} configured={s.hasTmdbApiKey} onSaved={load} />
      <InfoLanguageCard />
      <NumberingCard />
      <AirTimesCard />
      <ArtworkCard />
      <TrailerCard />
      <HardcoverCard />
      <TheTVDBCard />
      <OMDbCard />
      <GoogleBooksCard />
    </div>
  )
}
