import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, ApiError, type UsageStatsState } from '../api'
import { DOCS_URL, REPO_URL, WEBSITE_URL } from '../docs'
import { t } from '../i18n'
import { useFocusTrap } from '../useFocusTrap'
import Icon from './Icon'
import Switch from './Switch'
import { useToast } from './Toast'

// Anonymous usage numbers. Nothing is sent until an administrator presses Yes:
// the window below asks once (and again in a later session until it is answered),
// and Settings > Basics > General has the same choice any time. What can be sent is one
// fixed message, shown here exactly as it would go (internal/usagestats).

const LATER_KEY = 'mediarium-stats-later'
export const PAGE_URL = `${WEBSITE_URL}/stats/`
const SURVEY_URL = `${WEBSITE_URL}/survey/`
const CODE_URL = `${REPO_URL}/blob/main/internal/usagestats/usagestats.go`
const RECEIVER_URL = `${REPO_URL}/tree/main/tools/stats-receiver`
const DOCS_PAGE = `${DOCS_URL}/usage-stats.md`

function useUsageStats() {
  const [state, setState] = useState<UsageStatsState | null>(null)
  const toast = useToast()
  useEffect(() => {
    let live = true
    api
      .usageStats()
      .then((s) => live && setState(s))
      .catch(() => undefined)
    return () => {
      live = false
    }
  }, [])
  const choose = useCallback(
    async (choice: 'yes' | 'no') => {
      try {
        setState(await api.setUsageStats(choice))
        toast.success(choice === 'yes' ? t('Thank you. Anonymous numbers will be shared once a day.') : t('Nothing will be sent.'))
      } catch (e) {
        toast.error(e instanceof ApiError ? e.message : t("Couldn't save your choice."))
      }
    },
    [toast],
  )
  const resetId = useCallback(async () => {
    try {
      setState(await api.resetUsageStatsId())
      toast.success(t('Made a new random ID.'))
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : t("Couldn't make a new ID."))
    }
  }, [toast])
  return { state, choose, resetId }
}

// The message, as it would be sent now.
function Preview({ state }: { state: UsageStatsState }) {
  return (
    <details className="stats-preview">
      <summary>{t('See exactly what is sent')}</summary>
      <pre>{JSON.stringify(state.preview, null, 2)}</pre>
      <p className="hint">{t("This is this install's message right now, made by the same code that sends it. Before you say Yes, the ID is a placeholder.")}</p>
    </details>
  )
}

function Links() {
  return (
    <p className="hint stats-links">
      <a href={PAGE_URL} target="_blank" rel="noreferrer">
        {t('The public numbers')}
      </a>
      {' · '}
      <a href={DOCS_PAGE} target="_blank" rel="noreferrer">
        {t('The documentation')}
      </a>
      {' · '}
      <a href={CODE_URL} target="_blank" rel="noreferrer">
        {t('The code that makes the message')}
      </a>
      {' · '}
      <a href={RECEIVER_URL} target="_blank" rel="noreferrer">
        {t('The code that receives it')}
      </a>
    </p>
  )
}

// The question, for administrators, until it is answered.
export default function UsageStatsPrompt() {
  const { state, choose } = useUsageStats()
  const [later, setLater] = useState(() => {
    try {
      return sessionStorage.getItem(LATER_KEY) === '1'
    } catch {
      return false
    }
  })
  const [wait, setWait] = useState(true)
  useEffect(() => {
    const timer = window.setTimeout(() => setWait(false), 2500)
    return () => window.clearTimeout(timer)
  }, [])
  const open = !wait && !later && !!state && state.choice === '' && !state.disabled
  if (!open || !state) return null
  const askLater = () => {
    try {
      sessionStorage.setItem(LATER_KEY, '1')
    } catch {
      // the window simply comes back on the next page load
    }
    setLater(true)
  }
  return <AskDialog state={state} choose={choose} askLater={askLater} />
}

function AskDialog({ state, choose, askLater }: { state: UsageStatsState; choose: (c: 'yes' | 'no') => Promise<void>; askLater: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  useFocusTrap(box)
  return createPortal(
    <div className="modal-backdrop">
      <div ref={box} tabIndex={-1} className="modal stats-ask" role="dialog" aria-modal="true" aria-labelledby="stats-ask-title">
        <div className="confirm-head">
          <span className="confirm-ico">
            <Icon name="activity" size={20} />
          </span>
          <h2 id="stats-ask-title">{t('Help Mediarium with anonymous numbers?')}</h2>
        </div>
        <div className="confirm-body">
          <p>{t('Mediarium can send one small message a day that says how it is used, so the project knows how many installs are in use and which devices and setups are popular, and can make Mediarium work better on them. It is off until you choose. If you say No, nothing is ever sent.')}</p>
          <h3>{t('What is in the message')}</h3>
          <ul>
            <li>{t('A random ID made on this install, so one install counts once. It is not linked to you.')}</li>
            <li>{t('The Mediarium version, and whether it runs in Docker, on amd64 or arm64.')}</li>
            <li>{t('Rough bands, never exact numbers: processor cores, memory, how old the install is, how many titles, accounts and indexers.')}</li>
            <li>{t('Which features are switched on, such as Usenet, torrents or a media server, as yes or no.')}</li>
          </ul>
          <h3>{t('What is never sent')}</h3>
          <p>{t('Your name, titles, folders, host names, the names of your indexers or providers, passwords, anything you typed, or your address. Like any website, mediarium.app sees the address a message comes from while it arrives, and it is not saved with the numbers.')}</p>
          <h3>{t('The numbers are public')}</h3>
          <p>{t('The totals of everyone who said Yes are shown to everybody on mediarium.app/stats. All of this is in the open source code, so you can check it.')}</p>
          <Preview state={state} />
          <Links />
          <p className="hint">
            {t('Would rather not share from the app? You can tell us about your setup once, by hand, on the website. It is anonymous too.')}{' '}
            <a href={SURVEY_URL} target="_blank" rel="noreferrer">
              {t('Open the form')}
            </a>
          </p>
          <p className="hint">{t('You can change your mind at any time in Settings > Basics > General, or set MEDIARIUM_DISABLE_STATS=1 so it is never asked.')}</p>
        </div>
        <div className="modal-foot">
          <button type="button" className="link-btn" onClick={askLater}>
            {t('Ask me later')}
          </button>
          <button type="button" onClick={() => void choose('no')}>
            {t('No thanks')}
          </button>
          <button type="button" className="primary" onClick={() => void choose('yes')}>
            {t('Yes, share anonymous numbers')}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

// The same choice, in Settings > Basics > General.
export function UsageStatsCard() {
  const { state, choose, resetId } = useUsageStats()
  if (!state) return null
  const on = state.choice === 'yes'
  return (
    <fieldset className="group span-all">
      <legend>
        <Icon name="activity" size={14} /> {t('Anonymous usage numbers')}
      </legend>
      {state.disabled ? (
        <p>{t('This install never sends anything: MEDIARIUM_DISABLE_STATS is set.')}</p>
      ) : (
        <>
          <Switch
            checked={on}
            onChange={(v) => void choose(v ? 'yes' : 'no')}
            label={t('Share anonymous numbers once a day')}
            description={
              state.choice === ''
                ? t('Not answered yet. Nothing is sent until you switch this on.')
                : on
                  ? t('On. The totals of everyone who shares are public on mediarium.app/stats.')
                  : t('Off. Nothing is sent.')
            }
            showState
          />
          {on && (
            <p className="hint">
              {state.lastSent ? t('Last sent: {day}.', { day: state.lastSent }) : t('Not sent yet.')}{' '}
              <button type="button" className="link-btn" onClick={() => void resetId()}>
                {t('Reset my ID')}
              </button>
            </p>
          )}
        </>
      )}
      <Preview state={state} />
      <Links />
    </fieldset>
  )
}
