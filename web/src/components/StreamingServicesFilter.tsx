import Collapsible from './Collapsible'
import Switch from './Switch'
import { hidingSummary, SERVICES, setHideServices, toggleService, useMyServices } from '../streamingServices'
import { t } from '../i18n'

// Discover's "streaming services I have": pick them, then one switch leaves
// everything that can be watched on them out of the lists. Off to begin with.
export default function StreamingServicesFilter() {
  const mine = useMyServices()
  return (
    <div className="more-filters">
      <Collapsible id="discover-streaming-services" label={t('Streaming services')} showLabel summary={hidingSummary(mine.keys, mine.on)} forceOpen={mine.on}>
        <div className="services-filter">
          <p className="services-hint">{t('Pick the services you already pay for. With the switch on, titles you can watch on them in your country are left out.')}</p>
          <div className="chip-row" role="group" aria-label={t('Services I have')}>
            {SERVICES.map((s) => (
              <button key={s.key} type="button" className={`chip${mine.keys.includes(s.key) ? ' active' : ''}`} aria-pressed={mine.keys.includes(s.key)} onClick={() => toggleService(s.key)}>
                {s.name}
              </button>
            ))}
          </div>
          <Switch checked={mine.on} onChange={setHideServices} disabled={mine.keys.length === 0} label={t('Hide titles on my services')} />
        </div>
      </Collapsible>
    </div>
  )
}
