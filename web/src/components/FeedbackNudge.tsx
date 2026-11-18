import { useEffect, useState } from 'react'
import { demoEnabled } from '../demoFlag'
import { REPO_URL } from '../docs'
import { openReport } from '../report'
import { t } from '../i18n'
import { NUDGE_FIRST_SEEN, NUDGE_LAST_ASKED, NUDGE_NEVER, shouldNudge } from '../nudge'
import Icon from './Icon'
import ShareDialog from './ShareDialog'

function read(key: string): number | null {
  try {
    const v = Number(localStorage.getItem(key))
    return v > 0 ? v : null
  } catch {
    return null
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // blocked storage: it may ask again next time, which is harmless
  }
}

// Now and then (see nudge.ts) a small card in the corner asks for a GitHub
// star, an idea or a share. It waits half a minute so it never gets in the
// way of what someone opened the page for.
export default function FeedbackNudge() {
  const [show, setShow] = useState(false)
  const [sharing, setSharing] = useState(false)

  useEffect(() => {
    const now = Date.now()
    if (read(NUDGE_FIRST_SEEN) === null) write(NUDGE_FIRST_SEEN, String(now))
    let never = false
    try {
      never = localStorage.getItem(NUDGE_NEVER) === '1'
    } catch {
      never = true
    }
    if (demoEnabled() || !shouldNudge({ firstSeen: read(NUDGE_FIRST_SEEN), lastAsked: read(NUDGE_LAST_ASKED), never }, now)) return
    const timer = setTimeout(() => {
      write(NUDGE_LAST_ASKED, String(Date.now()))
      setShow(true)
    }, 30_000)
    return () => clearTimeout(timer)
  }, [])

  if (sharing) return <ShareDialog onClose={() => setSharing(false)} />
  if (!show) return null

  return (
    <aside className="nudge-card" role="complementary" aria-label={t('Enjoying Mediarium?')}>
      <button className="icon-btn nudge-close" onClick={() => setShow(false)} aria-label={t('Close')}>
        <Icon name="x" size={16} />
      </button>
      <h2>
        <Icon name="sparkles" size={18} /> {t('Enjoying Mediarium?')}
      </h2>
      <p>{t("It's made by one person in their spare time. A star on GitHub helps others find it, and your ideas shape what comes next.")}</p>
      <div className="nudge-actions">
        <a className="btn-link primary-look" href={REPO_URL} target="_blank" rel="noreferrer" onClick={() => setShow(false)}>
          <Icon name="star" size={15} /> {t('Star on GitHub')}
        </a>
        <button
          className="btn-with-icon"
          onClick={() => {
            setShow(false)
            openReport('idea')
          }}
        >
          <Icon name="chat" size={15} /> {t('Send an idea')}
        </button>
        <button
          className="btn-with-icon"
          onClick={() => {
            setShow(false)
            setSharing(true)
          }}
        >
          <Icon name="share" size={15} /> {t('Share')}
        </button>
      </div>
      <button
        className="link-btn nudge-never"
        onClick={() => {
          write(NUDGE_NEVER, '1')
          setShow(false)
        }}
      >
        {t("Don't ask again")}
      </button>
    </aside>
  )
}
