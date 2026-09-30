// Labels for the video buttons on a title page, from the kind of video the
// movie database says each one is: "Trailer 1", "Trailer 2", "Teaser", "Clip".
// A kind is only numbered when there is more than one of it.

export interface VideoLike {
  type?: string
}

const NAMES: Record<string, string> = {
  trailer: 'Trailer',
  teaser: 'Teaser',
  clip: 'Clip',
  featurette: 'Featurette',
  'behind the scenes': 'Behind the scenes',
  bloopers: 'Bloopers',
  opening_credits: 'Opening credits',
}

function kindName(type: string | undefined): string {
  const key = (type ?? '').trim().toLowerCase()
  if (!key) return 'Video'
  return NAMES[key] ?? key.charAt(0).toUpperCase() + key.slice(1).replace(/_/g, ' ')
}

export function trailerLabels(videos: VideoLike[]): string[] {
  const names = videos.map((v) => kindName(v.type))
  const totals = new Map<string, number>()
  for (const n of names) totals.set(n, (totals.get(n) ?? 0) + 1)
  const seen = new Map<string, number>()
  return names.map((n) => {
    if ((totals.get(n) ?? 0) < 2) return n
    const index = (seen.get(n) ?? 0) + 1
    seen.set(n, index)
    return `${n} ${index}`
  })
}
