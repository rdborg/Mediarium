import Switch from '../../components/Switch'
import { useAutosaveSetting } from '../../useAutosave'
import QualityProfilesSection from '../../components/QualityProfiles'

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
      <p style={{ color: 'var(--text-dim)' }}>
        Periodically searches for anything missing from your library and grabs the best match. Also checks each indexer's newest
        releases every few minutes, and looks for upgrades until an item reaches its profile's cutoff.
      </p>
      <Switch
        checked={automation.value}
        onChange={automation.change}
        disabled={!automation.loaded || automation.saving}
        label="Automatic searching and downloading"
        description="Saved as soon as you flip it."
      />
    </section>
  )
}

export default function QualitySettings() {
  return (
    <div className="settings-stack">
      <QualityProfilesSection />
      <AutomationSection />
    </div>
  )
}
