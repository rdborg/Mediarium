import { useState } from 'react'
import { MusicFallback } from './PosterCard'

// An album cover (or artist picture). Not every release has one on the Cover
// Art Archive, so a missing or broken image falls back to a plain sleeve.
export default function Cover({ src, alt = '', className }: { src?: string; alt?: string; className?: string }) {
  // Remember which address failed, so a new cover (the same card showing another album) gets its own try.
  const [brokenSrc, setBrokenSrc] = useState<string | undefined>()
  return (
    <span className={`cover${className ? ` ${className}` : ''}`}>
      {src && brokenSrc !== src ? <img src={src} alt={alt} loading="lazy" onError={() => setBrokenSrc(src)} /> : <MusicFallback />}
    </span>
  )
}
