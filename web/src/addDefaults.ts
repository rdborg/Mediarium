// What the add dialogs start with. Settings > Finding releases > Automatic searching has two
// choices, "Monitor new titles" and "Start searching when a title is added".
// Both are on unless switched off, which is how every install behaved before
// the settings existed. They only set the starting state: each title can still
// be changed in the dialog.
export interface AddDefaultsSource {
  addMonitored?: boolean
  addSearchNow?: boolean
  addKeepBetter?: boolean
}

export interface AddStart {
  monitored: boolean
  // The show choice: every episode, or none of them.
  monitor: 'all' | 'none'
  searchNow: boolean
}

// "Keep looking for better versions" starts from what this browser remembered from
// the last title added there; with nothing remembered, from Settings > Finding
// releases > Automatic searching. A missing answer (an older server, or a failed
// request) means off, which is how it always was.
export function keepBetterStart(src?: AddDefaultsSource | null, remembered?: boolean | null): boolean {
  return remembered ?? src?.addKeepBetter === true
}

// A missing answer (an older server, or a failed request) means on.
export function addStart(src?: AddDefaultsSource | null): AddStart {
  const monitored = src?.addMonitored !== false
  return { monitored, monitor: monitored ? 'all' : 'none', searchNow: src?.addSearchNow !== false }
}
