import Icon, { type IconName } from './Icon'
import type { Chosen } from '../setupHelpers'

type Key = keyof Chosen

const CARDS: { key: Key | 'audiobooks' | 'ebooks'; title: string; icon: IconName; text: string; soon?: boolean }[] = [
  { key: 'movies', title: 'Movies', icon: 'film', text: 'Find and download movies, and file them neatly in your library.' },
  { key: 'tv', title: 'TV shows', icon: 'tv', text: 'Follow shows and get new episodes when they air.' },
  { key: 'music', title: 'Music', icon: 'music', text: 'Keep your artists and albums in order.' },
  { key: 'audiobooks', title: 'Audiobooks', icon: 'headphones', text: 'You can set a folder for them in the next step.', soon: true },
  { key: 'ebooks', title: 'Ebooks', icon: 'book', text: 'You can set a folder for them in the next step.', soon: true },
]

// "What do you want to manage?": one card per kind of media. Movies, TV and
// music switch on and off. Audiobooks and ebooks are on the way, so they are
// shown but cannot be switched on yet.
export default function SetupMediaTypes({ chosen, onToggle, lastOneHint }: { chosen: Chosen; onToggle: (key: Key) => void; lastOneHint: boolean }) {
  return (
    <div className="media-cards">
      {CARDS.map((c) => {
        const on = !c.soon && chosen[c.key as Key]
        return (
          <button
            key={c.key}
            type="button"
            role="checkbox"
            aria-checked={on}
            aria-disabled={c.soon || undefined}
            disabled={c.soon}
            className={`choice-card media-card${on ? ' active' : ''}${c.soon ? ' soon' : ''}`}
            onClick={() => !c.soon && onToggle(c.key as Key)}
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
