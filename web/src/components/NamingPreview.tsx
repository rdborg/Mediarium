import { useEffect, useState } from 'react'
import { api } from '../api'

// Live filename preview: shows the exact resulting filename as the user
// edits the tokens. Renders through the real
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
    <small className="naming-preview">
      Preview: <code>{preview.folder}/{preview.filename}</code>
    </small>
  )
}
