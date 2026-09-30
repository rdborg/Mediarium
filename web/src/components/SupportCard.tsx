import { useState } from 'react'
import { api } from '../api'
import { formatSupportReport } from '../supportReport'
import { copyText } from './CopyBox'
import Icon from './Icon'

// Settings > System: one button that copies a report for a support request, so
// nobody has to dig the log out of the NAS's log viewer.
export default function SupportCard() {
  const [state, setState] = useState<'idle' | 'working' | 'copied' | 'failed'>('idle')
  const [report, setReport] = useState('')
  const [error, setError] = useState('')

  async function copy() {
    setState('working')
    setError('')
    try {
      const text = formatSupportReport(await api.diagnostics())
      setReport(text)
      setState((await copyText(text)) ? 'copied' : 'failed')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setState('idle')
    }
  }

  return (
    <fieldset className="group alerts span-all">
      <legend>
        <Icon name="chat" size={14} /> Help and support
      </legend>
      <p style={{ marginTop: 0 }}>
        If something is not working, copy this report and paste it into your support request. It has your version, how the database is doing, the setup warnings, the latest problems from Logs and errors and the last 500 lines of the log. Passwords, keys and tokens are left out.
      </p>
      <button type="button" className="primary btn-with-icon" onClick={() => void copy()} disabled={state === 'working'}>
        <Icon name={state === 'copied' ? 'check' : 'list'} size={15} />{' '}
        {state === 'working' ? 'Getting the report…' : state === 'copied' ? 'Copied' : 'Copy for support'}
      </button>
      {error && <p className="error-text">{error}</p>}
      {state === 'failed' && (
        <>
          <p style={{ color: 'var(--text-dim)' }}>Your browser did not let Mediarium copy it. Select the text below and copy it yourself.</p>
          <textarea readOnly value={report} rows={10} style={{ width: '100%', fontFamily: 'monospace', fontSize: '0.8rem' }} onFocus={(e) => e.currentTarget.select()} />
        </>
      )}
    </fieldset>
  )
}
