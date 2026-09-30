// Subtitle languages offered in the UI, as OpenSubtitles language codes.
export const SUBTITLE_LANGUAGES: { code: string; name: string }[] = [
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Spanish' },
  { code: 'fr', name: 'French' },
  { code: 'de', name: 'German' },
  { code: 'it', name: 'Italian' },
  { code: 'pt-PT', name: 'Portuguese (Portugal)' },
  { code: 'pt-BR', name: 'Portuguese (Brazil)' },
  { code: 'nl', name: 'Dutch' },
  { code: 'sv', name: 'Swedish' },
  { code: 'da', name: 'Danish' },
  { code: 'fi', name: 'Finnish' },
  { code: 'no', name: 'Norwegian' },
  { code: 'pl', name: 'Polish' },
  { code: 'cs', name: 'Czech' },
  { code: 'hu', name: 'Hungarian' },
  { code: 'ro', name: 'Romanian' },
  { code: 'el', name: 'Greek' },
  { code: 'tr', name: 'Turkish' },
  { code: 'ru', name: 'Russian' },
  { code: 'ar', name: 'Arabic' },
  { code: 'he', name: 'Hebrew' },
  { code: 'ja', name: 'Japanese' },
  { code: 'ko', name: 'Korean' },
  { code: 'zh-CN', name: 'Chinese (Simplified)' },
  { code: 'zh-TW', name: 'Chinese (Traditional)' },
]

export const languageName = (code: string) => SUBTITLE_LANGUAGES.find((l) => l.code === code)?.name ?? code

// Audio languages a release can be in, by the names the server uses for the
// "Preferred audio language" setting (English first).
export const AUDIO_LANGUAGES: string[] = [
  'English', 'Arabic', 'Chinese', 'Czech', 'Danish', 'Dutch', 'Finnish', 'French', 'German', 'Greek', 'Hebrew', 'Hindi',
  'Hungarian', 'Italian', 'Japanese', 'Korean', 'Nordic', 'Norwegian', 'Polish', 'Portuguese', 'Romanian', 'Russian',
  'Spanish', 'Swedish', 'Tamil', 'Telugu', 'Thai', 'Turkish', 'Ukrainian', 'Vietnamese',
]
