import Icon, { type IconName } from './Icon'
import type { Chosen } from '../setupHelpers'

type Key = keyof Chosen

const CARDS: { key: Key; title: string; icon: IconName; text: string; soon?: boolean }[] = [
  { key: 'movies', title: 'Movies', icon: 'film', text: 'Find and download movies, and file them neatly in your library.' },
  { key: 'tv', title: 'TV shows', icon: 'tv', text: 'Follow shows and get new episodes when they air.' },
  { key: 'music', title: 'Music', icon: 'music', text: 'Keep your artists and albums in order.' },
  { key: 'ebooks', title: 'Ebooks', icon: 'book', text: 'Books for your e-reader, EPUB first.' },
  { key: 'audiobooks', title: 'Audiobooks', icon: 'headphones', text: 'Audiobooks, filed by author, ready for your player.' },
]

// "What do you want to manage?": one card per kind of media, each one
// switched on or off.
export default function SetupMediaTypes({ chosen, onToggle, lastOneHint }: { chosen: Chosen; onToggle: (key: Key) => void; lastOneHint: boolean }) {
  return (
    <div className="media-cards">
      {CARDS.map((c) => {
        const on = !c.soon && !!chosen[c.key]
        return (
          <button
            key={c.key}
            type="button"
            role="checkbox"
            aria-checked={on}
            aria-disabled={c.soon || undefined}
            disabled={c.soon}
            className={`choice-card media-card${on ? ' active' : ''}${c.soon ? ' soon' : ''}`}
            onClick={() => !c.soon && onToggle(c.key)}
          >
            <span className="choice-box" aria-hidden="true">
              {on && <Icon name="check" size={13} />}
            </span>
            <span className="media-card-ico" aria-hidden="true">
              <Icon name={c.icon} size={22} />
            </span>
            <strong>
              {c.title}
              {c.soon && <span className="fbox-tag">Coming soon</span>}
            </strong>
            <small>{c.text}</small>
          </button>
        )
      })}
      {lastOneHint && <p className="media-cards-note">At least one has to stay on.</p>}
    </div>
  )
}
