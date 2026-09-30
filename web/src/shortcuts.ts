// Keyboard shortcuts that work anywhere in the app. Kept apart so the rule for
// "are you typing?" can be tested.

// True when a key press is going into something you type in, so a shortcut
// must leave it alone.
export function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as { tagName?: string; isContentEditable?: boolean; type?: string } | null
  if (!el || typeof el.tagName !== 'string') return false
  const tag = el.tagName.toLowerCase()
  if (tag === 'textarea' || tag === 'select') return true
  if (tag === 'input') {
    // Boxes you tick or press are not typing.
    const type = (el.type ?? 'text').toLowerCase()
    return !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color', 'file', 'image'].includes(type)
  }
  return el.isContentEditable === true
}
