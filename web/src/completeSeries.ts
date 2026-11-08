// The "Search for the complete series" button of a show.

/**
 * Whether a show offers the button. A show that has ended always does: its packs
 * are all out. One that is still running does once it has more than one season,
 * since the earlier seasons are finished and may come as packs.
 */
export function offersCompleteSearch(showStatus: string | undefined, seasonCount: number): boolean {
  if (showStatus === 'ended' || showStatus === 'canceled') return seasonCount > 0
  return seasonCount > 1
}
