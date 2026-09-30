// What kind of act MusicBrainz says an artist is, in plain words.
export function artistKind(type?: string): string {
  switch ((type ?? '').toLowerCase()) {
    case 'group':
      return 'Band'
    case 'person':
      return 'Solo artist'
    case 'orchestra':
      return 'Orchestra'
    case 'choir':
      return 'Choir'
    case 'character':
      return 'Character'
    default:
      return 'Artist'
  }
}
