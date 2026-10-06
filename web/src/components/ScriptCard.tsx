import { useCallback, useEffect, useState } from 'react'
import { api, type ScriptRun, type ScriptsState } from '../api'
import { DOCS_URL } from '../docs'
import { timeAgo } from '../format'
import Icon from './Icon'
import { useToast } from './Toast'

// Settings > System: a script of your own to run after each import. Only
// files in the scripts folder inside the config folder can be picked, and
// only here (not with an API key). Off until one is picked.
export default function ScriptCard() {
  const toast = useToast()
  const [state, setState] = useState<ScriptsState | null>(null)
  const [script, setScript] = useState('')
  const [minutes, setMinutes] = useState(5)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    api
      .getScripts()
      .then((s) => {
        setState(s)
        setScript(s.script)
        setMinutes(Math.max(1, Math.round(s.timeoutSec / 60)))
      })
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
  }, [])
  useEffect(load, [load])

  async function save() {
    setBusy(true)
    setError('')
    try {
      const s = await api.putScripts({ script, timeoutSec: Math.round(minutes * 60) })
      setState(s)
      toast.success(s.script ? `${s.script} will run after each import.` : 'No script runs after imports.')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  async function tryIt() {
    setBusy(true)
    setError('')
    try {
      const run = await api.testScript()
      setState((cur) => (cur ? { ...cur, lastRun: run } : cur))
      if (run.problem) toast.error(`${run.script}: ${run.problem}`)
      else toast.success(`${run.script} ran fine.`)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const saved = state?.script ?? ''
  const changed = state !== null && (script !== saved || Math.round(minutes * 60) !== state.timeoutSec)
  return (
    <fieldset className="group">
      <legend>
        <Icon name="flask" size={14} /> Run a script after each import
      </legend>
      <p style={{ color: 'var(--text-dim)', marginTop: 0 }}>
        For your own extra steps, such as telling another app or copying a file somewhere. Put the script in{' '}
        <code>{state?.folder ?? '/config/scripts'}</code>, make it executable, and pick it here. It is told what was imported in <code>MEDIARIUM_*</code>{' '}
        variables. Only scripts in that folder can be picked, and only from this page.{' '}
        <a href={`${DOCS_URL}/scripts.md`} target="_blank" rel="noreferrer">
          What a script gets
        </a>
      </p>
      <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'end' }}>
        <label style={{ display: 'grid', gap: 4, flex: '1 1 220px', maxWidth: 320 }}>
          <span>Script</span>
          <select value={script} onChange={(e) => setScript(e.target.value)} disabled={busy || state === null}>
            <option value="">None (off)</option>
            {state?.scripts.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
            {saved && !state?.scripts.includes(saved) && <option value={saved}>{saved} (missing)</option>}
          </select>
        </label>
        <label style={{ display: 'grid', gap: 4, width: 170 }}>
          <span>Time limit (minutes)</span>
          <input type="number" min={1} max={60} value={minutes} onChange={(e) => setMinutes(Math.min(60, Math.max(1, Number(e.target.value) || 1)))} disabled={busy || state === null} />
        </label>
        <button className="primary" onClick={() => void save()} disabled={busy || !changed}>
          Save
        </button>
        <button onClick={() => void tryIt()} disabled={busy || !saved || changed} title="Runs it once with a made-up movie and MEDIARIUM_EVENT=test">
          Try it
        </button>
        <button className="ghost btn-with-icon" onClick={load} disabled={busy} title="Look in the folder again">
          <Icon name="refresh" size={14} /> Refresh list
        </button>
      </div>
      {state !== null && state.scripts.length === 0 && <p className="field-hint">The folder has no executable scripts yet.</p>}
      {state?.lastRun && <LastRun run={state.lastRun} />}
      {error && <p className="error-text">{error}</p>}
    </fieldset>
  )
}

function LastRun({ run }: { run: ScriptRun }) {
  const what = run.event === 'test' ? 'a try' : run.title ? `after importing ${run.title}` : 'after an import'
  return (
    <div className="field-hint" style={{ marginTop: 10 }}>
      <span style={{ color: run.problem ? 'var(--danger)' : undefined }}>
        Last run: {run.script}, {what}, {timeAgo(run.at)}. {run.problem || `It took ${run.seconds} s and finished fine.`}
      </span>
      {run.output && <pre style={{ whiteSpace: 'pre-wrap', maxHeight: 200, overflow: 'auto', marginTop: 6 }}>{run.output}</pre>}
    </div>
  )
}
