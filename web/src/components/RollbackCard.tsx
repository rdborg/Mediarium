import { useEffect, useState } from 'react'
import { api, type RollbackState } from '../api'
import { formatBytes } from '../format'
import { locale, t } from '../i18n'
import { reloadWhenBack } from '../reloadWhenBack'
import { useConfirm } from './ConfirmProvider'
import Icon from './Icon'
import TipLabel from './TipLabel'
import { useToast } from './Toast'

// Settings > General: going back after an update that went wrong. Before an
// update changes the database, Mediarium keeps a copy of it; putting that copy
// back (and the program from before, when an update was installed from here)
// is what an older version needs to run again. Hidden while there is nothing
// to go back to.
export default function RollbackCard() {
  const toast = useToast()
  const confirm = useConfirm()
  const [state, setState] = useState<RollbackState | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    api.rollbackState().then(setState).catch(() => undefined)
  }, [])

  if (!state || (state.copies.length === 0 && !state.previous)) return null
  const when = (at: string) => new Date(at).toLocaleString(locale(), { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })

  // The program from before goes with the newest copy only: an older copy may
  // be from an update before that one.
  async function goBack(copy: RollbackState['copies'][number] | null, newest: boolean) {
    const program = !!state?.previous && newest
    const ok = await confirm({
      title: t('Go back to before the update?'),
      body: (
        <>
          {copy && (
            <p>
              {t('Your database is put back as it was on {when}. Anything that changed since then is lost: titles added, downloads that finished, settings you changed.', { when: when(copy.at) })}{' '}
              {t('The database in use now is kept in a before-restore folder in your config folder, so nothing is deleted.')}
            </p>
          )}
          {program ? (
            <p>{t('Version {version} starts again.', { version: state?.previous ?? '' })}</p>
          ) : (
            <p>{t('Then start the older version: change the image tag in your compose file to that version and recreate the container. If this version starts again instead, it updates the database again.')}</p>
          )}
          <p>{t('Mediarium restarts now.')}</p>
        </>
      ),
      confirmLabel: t('Go back and restart'),
      danger: true,
    })
    if (!ok) return
    setBusy(true)
    try {
      await api.rollback({ copy: copy?.name, program })
      toast.success(t('Going back. Restarting…'))
      void reloadWhenBack(program ? state?.previous : undefined)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }

  return (
    <fieldset className="group usenet span-all">
      <legend>
        <Icon name="refresh" size={14} /> <TipLabel help="backups-updates#going-back-after-an-update">{t('Going back after an update')}</TipLabel>
      </legend>
      <p className="field-hint" style={{ marginTop: 0 }}>
        {t('Before an update changes the database, Mediarium keeps a copy of it. If an update goes wrong, put the copy back to run the version you had before.')}
      </p>
      <ul className="rollback-list">
        {state.copies.map((c, i) => (
          <li key={c.name}>
            <span>
              <strong>{when(c.at)}</strong>
              <small>{c.version ? t('Used by version {version}', { version: c.version }) : t('From an earlier version')} · {formatBytes(c.size)}</small>
            </span>
            <button className="danger-ghost btn-sm" disabled={busy} onClick={() => void goBack(c, i === 0)}>
              {t('Go back to this')}
            </button>
          </li>
        ))}
        {state.copies.length === 0 && state.previous && (
          <li>
            <span>
              <strong>{t('Version {version}', { version: state.previous })}</strong>
              <small>{t('The program from before the installed update')}</small>
            </span>
            <button className="danger-ghost btn-sm" disabled={busy} onClick={() => void goBack(null, true)}>
              {t('Go back to this')}
            </button>
          </li>
        )}
      </ul>
    </fieldset>
  )
}
