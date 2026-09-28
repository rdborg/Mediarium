import { useEffect, useState } from 'react'
import { api } from '../api'

// Live filename preview (PRD §4.8: "a live preview in the UI showing the
// exact resulting filename as they edit the tokens" — asked for twice in
// PRD.md and missing entirely until now). Renders through the real
// backend naming engine rather than reimplementing token substitution in
// JS, so it can never drift from what the pipeline actually produces.
export default function NamingPreview({ preset, format }: { preset: string; format?: string }) {
  const [preview, setPreview] = useState<{ folder: string; filename: string } | null>(null)

  useEffect(() => {
    const timeout = setTimeout(() => {
      api.namingPreview(preset, format).then(setPreview).catch(() => setPreview(null))
    }, 300)
    return () => clearTimeout(timeout)
  }, [preset, format])

  if (!preview) return null

  return (
    <p style={{ color: 'var(--text-dim)', fontSize: '0.85rem', fontFamily: 'var(--font-mono)' }}>
      Preview: {preview.folder}/{preview.filename}
    </p>
  )
}
