import AutomationIntervals from '../../components/AutomationIntervals'
import Dropdown from '../../components/Dropdown'
import Switch from '../../components/Switch'
import { AUDIO_LANGUAGES } from '../../languages'
import { useAutosaveSetting } from '../../useAutosave'
import QualityProfilesSection from '../../components/QualityProfiles'
import MusicProfilesSection from '../../components/MusicProfiles'

function AutomationSection() {
  const automation = useAutosaveSetting<boolean>(
    (s) => s.automationEnabled ?? true,
    (v) => ({ automationEnabled: v }),
    (saved) => (saved ? 'Saved: automatic searching is on.' : 'Saved: automatic searching is off.'),
    true,
  )

  return (
    <section className="card">
      <h2>Automation</h2>
      <Switch
        checked={automation.value}
        onChange={automation.change}
        disabled={!automation.loaded || automation.saving}
        label="Search and download automatically"
        description="Looks for missing items, new releases and better versions in the background."
        showState
      />
      <AutomationIntervals />
    </section>
  )
}

function LanguageSection() {
  const language = useAutosaveSetting<string>(
    (s) => s.qualityLanguage || 'English',
    (v) => ({ qualityLanguage: v }),
    (saved) => `Saved: looking for ${saved} releases.`,
    'English',
  )

  return (
    <section className="card">
      <h2>Audio language</h2>
      <p style={{ color: 'var(--text-dim)' }}>
        Releases with no language in the name count as English. Releases only in another language, like GERMAN, are skipped.
      </p>
      <label className="field" style={{ maxWidth: 280 }}>
        <span>Preferred audio language</span>
        <Dropdown
          label="Preferred audio language"
          value={language.value}
          options={AUDIO_LANGUAGES.map((l) => ({ value: l, label: l }))}
          onChange={(v) => void language.change(v)}
          disabled={!language.loaded || language.saving}
        />
      </label>
    </section>
  )
}

export default function QualitySettings() {
  return (
    <div className="settings-stack">
      <QualityProfilesSection />
      <LanguageSection />
      <AutomationSection />
      <MusicProfilesSection />
    </div>
  )
}
