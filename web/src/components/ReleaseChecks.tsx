import { st, t } from '../i18n'
import type { WhyCheck } from '../releaseWhy'

// What was checked on a release against a quality profile, one line each: a
// tick when it passed, a cross when it is why an automatic search leaves the
// release out, a dot for a plain note. Used under a release in Choose a release
// and in the profile simulator.
export default function ReleaseChecks({ checks }: { checks: WhyCheck[] }) {
  return (
    <ul className="rel-why-checks">
      {checks.map((c, i) => (
        <li key={i} className={`rel-why-check ${c.state}`}>
          <span className="rel-why-mark" aria-hidden="true">
            {c.state === 'ok' ? '✓' : c.state === 'bad' ? '✗' : '•'}
          </span>
          <span className="sr-only">{c.state === 'ok' ? t('Passed') : c.state === 'bad' ? t('Failed') : t('Note')}: </span>
          <span>{st(c.text)}</span>
        </li>
      ))}
    </ul>
  )
}
