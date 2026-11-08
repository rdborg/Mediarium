import { useState } from 'react'
import AutomationIntervals from '../../components/AutomationIntervals'
import CustomFormatsSection from '../../components/CustomFormats'
import DelayProfileCard from '../../components/DelayProfileCard'
import Dropdown from '../../components/Dropdown'
import Icon from '../../components/Icon'
import Switch from '../../components/Switch'
import { AUDIO_LANGUAGES } from '../../languages'
import { useAutosaveSetting } from '../../useAutosave'
import QualityProfilesSection from '../../components/QualityProfiles'
import ProfileSimulator from '../../components/ProfileSimulator'
import SizeLimitsCard from '../../components/SizeLimitsCard'
import MusicProfilesSection from '../../components/MusicProfiles'
import MusicFiltersCard from '../../components/MusicFiltersCard'
import QuietForAWhile from '../../components/QuietForAWhile'
import { t } from '../../i18n'
import TipLabel from '../../components/TipLabel'

function AutomationSection() {
  const automation = useAutosaveSetting<boolean>(
    (s) => s.automationEnabled ?? true,
    (v) => ({ automationEnabled: v }),
    (saved) => (saved ? t('Saved: automatic searching is on.') : t('Saved: automatic searching is off.')),
    true,
  )
  const unmonitorDownloaded = useAutosaveSetting<boolean>(
    (s) => s.unmonitorDownloaded ?? false,
    (v) => ({ unmonitorDownloaded: v }),
    (saved) => (saved ? t('Saved: movies stop being monitored once downloaded.') : t('Saved: downloaded movies stay monitored.')),
    false,
  )
  const unmonitorFinished = useAutosaveSetting<boolean>(
    (s) => s.unmonitorFinished ?? false,
    (v) => ({ unmonitorFinished: v }),
    (saved) => (saved ? t('Saved: finished shows stop being monitored once complete.') : t('Saved: finished shows stay monitored.')),
    false,
  )
  const holdUnreleased = useAutosaveSetting<boolean>(
    (s) => s.holdUnreleased ?? true,
    (v) => ({ holdUnreleased: v }),
    (saved) => (saved ? t('Saved: automatic searches wait until a title is out.') : t('Saved: automatic searches also look for titles that are not out yet.')),
    true,
  )
  const seasonPacks = useAutosaveSetting<boolean>(
    (s) => s.seasonPacks ?? false,
    (v) => ({ seasonPacks: v }),
    (saved) => (saved ? t('Saved: season packs are preferred once a season has ended.') : t('Saved: episodes are fetched one by one.')),
    false,
  )

  return (
    <section className="card">
      <h2><TipLabel help="quality#automatic-searching">{t('Automation')}</TipLabel></h2>
      <Switch
        checked={automation.value}
        onChange={automation.change}
        disabled={!automation.loaded || automation.saving}
        label={t('Search and download automatically')}
        description={t('Looks for missing items, new releases and better versions in the background.')}
        showState
      />
      <AutomationIntervals />
      <QuietForAWhile />
      <Switch
        checked={unmonitorDownloaded.value}
        onChange={unmonitorDownloaded.change}
        disabled={!unmonitorDownloaded.loaded || unmonitorDownloaded.saving}
        label={t("Stop monitoring a movie once it's downloaded")}
        tip={t("Monitored means Mediarium searches for it. You can switch monitoring back on from the movie's page at any time.")}
        learnMore="quality#automatic-searching"
        description={t("It won't be searched again, not even for a better version, and a file you delete isn't fetched again.")}
      />
      <Switch
        checked={unmonitorFinished.value}
        onChange={unmonitorFinished.change}
        disabled={!unmonitorFinished.loaded || unmonitorFinished.saving}
        label={t('Stop monitoring a show once it has ended and every episode is in')}
        description={t("Checked when the show's details are refreshed and after each import. Shows that are still running stay monitored.")}
      />
      <Switch
        checked={holdUnreleased.value}
        onChange={holdUnreleased.change}
        disabled={!holdUnreleased.loaded || holdUnreleased.saving}
        label={t("Wait until a title is out before searching")}
        description={t("A movie isn't searched for before its release date and an episode isn't searched for before it airs. The title's history says what it is waiting for. Search now always looks.")}
        showState
      />
      <Switch
        checked={seasonPacks.value}
        onChange={seasonPacks.change}
        disabled={!seasonPacks.loaded || seasonPacks.saving}
        label={t('Prefer season packs once a season has ended')}
        description={t("While a season is airing, episodes are fetched one by one. Once it has ended, Mediarium looks for a season pack and takes it if it's the same quality or better than the episodes. A show can choose for itself on its page.")}
      />
    </section>
  )
}

// "Original" in the list stands for the language each film or show was made in.
const ORIGINAL = 'Original'

function LanguageSection() {
  const language = useAutosaveSetting<string>(
    (s) => s.qualityLanguage || 'English',
    (v) => ({ qualityLanguage: v }),
    (saved) => t('Saved: audio languages are {language}.', { language: saved.split(',').map((l) => (l === ORIGINAL ? t('Original language') : l)).join(', ') }),
    'English',
  )
  const only = useAutosaveSetting<boolean>(
    (s) => s.qualityLanguageOnly ?? false,
    (v) => ({ qualityLanguageOnly: v }),
    (saved) => (saved ? t('Saved: only releases in these languages are taken.') : t('Saved: releases with no language in the name are taken too when nothing else is found.')),
    false,
  )
  const list = language.value.split(',').map((l) => l.trim()).filter(Boolean)
  const name = (l: string) => (l === ORIGINAL ? t('Original language of the title') : l)
  const free = [ORIGINAL, ...AUDIO_LANGUAGES].filter((l) => !list.includes(l))
  const busy = !language.loaded || language.saving
  const save = (next: string[]) => void language.change(next.join(',') || 'English')
  const move = (i: number, by: number) => {
    const next = [...list]
    ;[next[i], next[i + by]] = [next[i + by], next[i]]
    save(next)
  }

  return (
    <section className="card">
      <h2><TipLabel help="quality#audio-language">{t('Audio language')}</TipLabel></h2>
      <p style={{ color: 'var(--text-dim)' }}>
        {t("List the languages you'd watch in, best first. A release in an earlier language is picked over one in a later language, and releases with two or more audio tracks count when one of them matches. With one language, a release that names only that language is picked over a multi-language one. With several, like German and English, a multi-language release that has the first one is picked over a plain one, because it also carries the others. Releases with no language in the name count as English. Releases only in some other language, like GERMAN, are skipped.")}
      </p>
      <ol className="lang-order">
        {list.map((l, i) => (
          <li key={l}>
            <span className="lang-n">{i + 1}</span>
            <span className="lang-name">{name(l)}</span>
            <button type="button" className="icon-btn" disabled={busy || i === 0} onClick={() => move(i, -1)} aria-label={t('Move {name} up', { name: name(l) })} title={t('Move up')}>
              <Icon name="chevron-up" size={14} />
            </button>
            <button type="button" className="icon-btn" disabled={busy || i === list.length - 1} onClick={() => move(i, 1)} aria-label={t('Move {name} down', { name: name(l) })} title={t('Move down')}>
              <Icon name="chevron-down" size={14} />
            </button>
            <button type="button" className="icon-btn" disabled={busy || list.length === 1} onClick={() => save(list.filter((x) => x !== l))} aria-label={t('Remove {name}', { name: name(l) })} title={t('Remove')}>
              <Icon name="x" size={14} />
            </button>
          </li>
        ))}
      </ol>
      {free.length > 0 && (
        <label className="field" style={{ maxWidth: 320 }}>
          <span>{t('Add a language')}</span>
          <Dropdown
            label={t('Add a language')}
            value=""
            options={[{ value: '', label: t('Choose a language') }, ...free.map((l) => ({ value: l, label: name(l) }))]}
            onChange={(v) => v && save([...list, v])}
            disabled={busy}
          />
        </label>
      )}
      <Switch
        checked={only.value}
        onChange={only.change}
        disabled={!only.loaded || only.saving}
        label={t('Only take releases in these languages')}
        description={t("Off, a release with no language in the name (counted as English) or tagged English only is still taken when nothing better turns up. On, it is skipped unless English is on your list, and so is a MULTi or DUAL release that doesn't name one of your languages. You can still pick any release yourself.")}
        showState
      />
    </section>
  )
}

function AdvancedSection({ value, loaded, saving, change }: { value: boolean; loaded: boolean; saving: boolean; change: (v: boolean) => void }) {
  return (
    <section className="card">
      <h2><TipLabel help="quality#custom-formats">{t('Advanced options')}</TipLabel></h2>
      <Switch
        checked={value}
        onChange={change}
        disabled={!loaded || saving}
        label={t('Show advanced options')}
        description={t("Custom formats and format scores, as in Radarr and Sonarr, with the TRaSH Guides. Most people don't need them: the quality and Skip bad releases already get good downloads.")}
        showState
      />
    </section>
  )
}

export default function QualitySettings() {
  // Bumped when formats or profiles change, so the other card reloads.
  const [profilesRev, setProfilesRev] = useState(0)
  const [formatsRev, setFormatsRev] = useState(0)
  const advanced = useAutosaveSetting<boolean>(
    (s) => s.showAdvanced ?? false,
    (v) => ({ showAdvanced: v }),
    (saved) => (saved ? t('Saved: custom formats are shown.') : t('Saved: custom formats are hidden.')),
    false,
  )
  return (
    <div className="settings-stack">
      <QualityProfilesSection rev={profilesRev} onChanged={() => setFormatsRev((n) => n + 1)} advanced={advanced.value} />
      <ProfileSimulator />
      {advanced.value && <CustomFormatsSection rev={formatsRev} onChanged={() => setProfilesRev((n) => n + 1)} />}
      <LanguageSection />
      <SizeLimitsCard />
      <MusicProfilesSection />
      <MusicFiltersCard />
      <AdvancedSection value={advanced.value} loaded={advanced.loaded} saving={advanced.saving} change={(v) => void advanced.change(v)} />
    </div>
  )
}

// The third Library page: when Mediarium looks for things by itself, and
// whether it waits for a Usenet or torrent release.
export function AutomationSettings() {
  return (
    <div className="settings-stack">
      <AutomationSection />
      <DelayProfileCard />
    </div>
  )
}
