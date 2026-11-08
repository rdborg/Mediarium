import { useEffect, useState } from 'react'
import { t } from '../i18n'
import { foldInUse, readFacts, savedFold, savedSide, savedTop, setFold, setFoldSide, setFoldTop, type DisplayFacts, type FoldChoice } from '../fold'
import Icon from './Icon'

// Settings > General: the foldable and dual-screen layout, and what this screen
// reports (so the layout can be tuned for the phone in hand).
export default function FoldSettings() {
  const [choice, setChoice] = useState<FoldChoice>(savedFold)
  const [top, setTop] = useState(savedTop)
  const [side, setSide] = useState(savedSide)
  const [facts, setFacts] = useState<DisplayFacts>(() => readFacts())
  useEffect(() => {
    const update = () => setFacts(readFacts())
    window.addEventListener('resize', update)
    window.addEventListener('orientationchange', update)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('orientationchange', update)
    }
  }, [])
  const i = facts.insets
  return (
    <fieldset className="group">
      <legend>
        <Icon name="sliders" size={14} /> {t('Foldable and dual-screen')}
      </legend>
      <p style={{ margin: '0 0 10px', color: 'var(--text-dim)' }}>
        {t('Use the whole screen of a foldable phone. The header sits in the top strip beside the clock and battery icons, and held sideways the theme and profile buttons move into the side strip.')}
      </p>
      <div className="grid-form">
        <label className="inline-field">
          {t('Foldable layout')}
          <select
            value={choice}
            onChange={(e) => {
              const v = e.target.value as FoldChoice
              setFold(v)
              setChoice(v)
              setFacts(readFacts())
            }}
          >
            <option value="">{t('Automatic')}</option>
            <option value="on">{t('On')}</option>
            <option value="off">{t('Off')}</option>
          </select>
        </label>
        <label className="inline-field">
          {t('Space kept free for the system icons at the top: {px} px', { px: top })}
          <input
            type="range"
            min={60}
            max={300}
            step={5}
            value={top}
            onChange={(e) => {
              const v = Number(e.target.value)
              setFoldTop(v)
              setTop(v)
            }}
          />
        </label>
        <label className="inline-field">
          {t('Space kept free for the system icons at the side: {px} px', { px: side })}
          <input
            type="range"
            min={60}
            max={300}
            step={5}
            value={side}
            onChange={(e) => {
              const v = Number(e.target.value)
              setFoldSide(v)
              setSide(v)
            }}
          />
        </label>
      </div>
      <ul className="fold-facts">
        <li>{t('Layout in use: {mode}', { mode: foldInUse(facts) ? t('Foldable') : t('Normal') })}</li>
        <li>{t('Screen {screen}, window {window}, pixel ratio {ratio}', { screen: `${facts.screenW} × ${facts.screenH}`, window: `${facts.innerW} × ${facts.innerH}`, ratio: facts.ratio })}</li>
        <li>{t('Safe area (top, right, bottom, left): {insets}', { insets: `${i.top}, ${i.right}, ${i.bottom}, ${i.left}` })}</li>
        <li>{t('Installed as an app: {state}', { state: facts.installed ? t('Yes') : t('No') })}</li>
      </ul>
    </fieldset>
  )
}
