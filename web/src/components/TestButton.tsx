import { useState } from 'react'

// A "Test" button that runs a connectivity check and shows the result
// inline: green for ok, red with the reason otherwise.
export default function TestButton({
  run,
  label = 'Test',
  disabled,
}: {
  run: () => Promise<{ ok: boolean; message: string }>
  label?: string
  disabled?: boolean
}) {
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null)

  async function click() {
    setBusy(true)
    setResult(null)
    try {
      setResult(await run())
    } catch (e) {
      setResult({ ok: false, message: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
      <button onClick={click} disabled={busy || disabled}>
        {busy ? 'Testing…' : label}
      </button>
      {result && (
        <span className={result.ok ? 'badge success' : 'error-text'} style={{ fontSize: '0.85rem' }}>
          {result.message}
        </span>
      )}
    </span>
  )
}
