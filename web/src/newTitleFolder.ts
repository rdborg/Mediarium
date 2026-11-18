// Which library folder a new movie or show goes to, when a kind has more
// than one folder (Settings > Library and files > Folders). The server says where a
// title added without a choice goes right now (GET /api/library/folders,
// newTitles): the Add dialog starts on that folder, and the forms that add
// titles later (a followed collection, an import list) can leave the choice
// to it. Settings has the rule itself: the main folder, one of the extra
// folders, or the folder with the most room.
import type { NewTitleFolder } from './api'
import { t } from './i18n.ts'

export interface FolderOption {
  value: string
  label: string
}

// The settings value for "whichever folder has the most room".
export const MOST_ROOM = 'most-room'

const mainLabel = (folder: string) => t('{folder} (main)', { folder })

// addChoices is the Add dialog's folder list: one option per folder, each
// with its own value (so the main folder is an explicit choice too), and the
// one the dialog starts on: where new titles go right now, else the main one.
export function addChoices(folders: string[], now?: NewTitleFolder): { options: FolderOption[]; initial: string } {
  const options = folders.map((f, i) => ({ value: f, label: i === 0 ? mainLabel(f) : f }))
  const initial = now && folders.includes(now.folder) ? now.folder : (folders[0] ?? '')
  return { options, initial }
}

// laterChoices is the list for a form that adds titles later: "" leaves the
// folder to the rule when each title is added, then the folders themselves.
export function laterChoices(folders: string[], now?: NewTitleFolder): FolderOption[] {
  const usual = now?.folder || folders[0] || ''
  return [{ value: '', label: t('Where new titles go ({folder} right now)', { folder: usual }) }, ...addChoices(folders).options]
}

// ruleChoices is the "New movies go to" list in Settings: the main folder,
// each extra folder, and the folder with the most room.
export function ruleChoices(main: string, extra: string[]): FolderOption[] {
  return [{ value: '', label: mainLabel(main) }, ...extra.map((f) => ({ value: f, label: f })), { value: MOST_ROOM, label: t('The folder with the most room') }]
}

// keptRule is the rule once the extra folders change: a rule naming a folder
// that is no longer in the list falls back to the main folder.
export function keptRule(rule: string, extra: string[]): string {
  return rule === '' || rule === MOST_ROOM || extra.includes(rule) ? rule : ''
}

// mostRoomNote is the line under the Add dialog's folder when the rule is
// "most room", so the preselected folder makes sense; "" otherwise.
export function mostRoomNote(now?: NewTitleFolder): string {
  return now?.mostRoom ? t('New titles go to the folder with the most room; right now that is {folder}.', { folder: now.folder }) : ''
}
