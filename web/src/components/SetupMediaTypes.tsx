import Icon from './Icon'
import KindGlyph, { type GlyphKind } from './KindGlyph'
import type { Chosen } from '../setupHelpers'
import { t } from '../i18n'

type Key = keyof Chosen

const CARDS: { key: Key; title: string; glyph: GlyphKind; text: string; soon?: boolean; later?: boolean }[] = [
  { key: 'movies', title: t('Movies'), glyph: 'movie', text: t('Find and download movies, and file them neatly in your library.') },
  { key: 'tv', title: t('TV shows'), glyph: 'tv', text: t('Follow shows and get new episodes when they air.') },
  { key: 'anime', title: t('Anime'), glyph: 'tv', text: t('Anime in a library of their own, inside TV. Optional.') },
  { key: 'music', title: t('Music'), glyph: 'music', text: t('Keep your artists and albums in order.') },
  { key: 'ebooks', title: t('Ebooks'), glyph: 'book', text: t('Books for your e-reader, EPUB first.') },
  { key: 'audiobooks', title: t('Audiobooks'), glyph: 'audiobook', text: t('Audiobooks, filed by author, ready for your player.') },
  { key: 'lifecycle', title: t('Library lifecycle'), glyph: 'movie', text: t('Remove what nobody watches, with a warning first. Optional.') },
  { key: 'archiver', title: t('Channel archiver'), glyph: 'tv', text: t('Keep YouTube channels with your own yt-dlp. Set it up in Settings.'), later: true },
]

// "What do you want to manage?": one card per kind of media, each one
// switched on or off.
export default function SetupMediaTypes({ chosen, onToggle, lastOneHint }: { chosen: Chosen; onToggle: (key: Key) => void; lastOneHint: boolean }) {
  return (
    <div className="media-cards">
      {CARDS.map((c) => {
        // The anime library is part of TV, so it can only be picked with TV.
        const off = c.soon || c.later || (c.key === 'anime' && !chosen.tv)
        const on = !off && !!chosen[c.key]
        return (
          <button
            key={c.key}
            type="button"
            role="checkbox"
            aria-checked={on}
            aria-disabled={off || undefined}
            disabled={off}
            className={`choice-card media-card k-${c.glyph}${on ? ' active' : ''}${c.soon ? ' soon' : ''}`}
            onClick={() => !off && onToggle(c.key)}
          >
            <span className="choice-box" aria-hidden="true">
              {on && <Icon name="check" size={13} />}
            </span>
            <span className="media-card-ico" aria-hidden="true">
              <KindGlyph kind={c.glyph} size={34} variant="solid" />
            </span>
            <strong>
              {c.title}
              {c.soon && <span className="fbox-tag">{t('Coming soon')}</span>}
              {c.later && <span className="fbox-tag">{t('Later')}</span>}
            </strong>
            <small>{c.text}</small>
          </button>
        )
      })}
      {lastOneHint && <p className="media-cards-note">{t('At least one has to stay on.')}</p>}
    </div>
  )
}
