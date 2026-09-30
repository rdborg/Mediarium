import type { QualityProfile } from '../api'

// A one-line, plain-language description of what a quality profile gets you.
export function profileBlurb(p: QualityProfile): string {
  const n = p.name.toLowerCase()
  if (n.startsWith('cinema')) return 'Recordings made in a cinema (CAM, TeleSync) and screeners. Poor picture and sound, so only worth it for a film you can\'t wait for.'
  if (n === 'any') return 'Takes the first thing it finds, even at low quality. Gets things fastest.'
  if (n.startsWith('720')) return 'Smaller files (about 1 to 4 GB a movie). Good for phones, tablets and slow connections.'
  if (n.startsWith('1080')) return 'Full HD, the sweet spot for most TVs (about 4 to 15 GB a movie). Recommended.'
  if (n.startsWith('4k') || n.includes('2160') || n.includes('ultra')) return 'Best picture for 4K TVs (about 15 to 60 GB a movie). Only takes 4K, so a title waits until a 4K release exists.'
  return `Up to ${p.cutoff}. A profile you made.`
}

// How good a profile aims to be, from its cutoff: 720p < 1080p < 4K.
function resolutionRank(p: QualityProfile): number {
  if (p.cutoff.startsWith('CAM')) return 0
  if (p.cutoff.includes('2160')) return 4
  if (p.cutoff.includes('1080')) return 3
  if (p.cutoff.includes('720')) return 2
  return 1
}

// Profiles from lowest to best resolution, with "Any" always last.
export function sortProfiles<T extends QualityProfile>(list: T[]): T[] {
  const isAny = (p: QualityProfile) => p.name.toLowerCase() === 'any'
  return [...list].sort((a, b) => Number(isAny(a)) - Number(isAny(b)) || resolutionRank(a) - resolutionRank(b) || a.name.localeCompare(b.name))
}
