import { t } from './i18n.ts'

// Subtitle languages offered in the UI, as OpenSubtitles language codes.
export const SUBTITLE_LANGUAGES: { code: string; name: string }[] = [
  { code: 'en', name: t('English') },
  { code: 'es', name: t('Spanish') },
  { code: 'fr', name: t('French') },
  { code: 'de', name: t('German') },
  { code: 'it', name: t('Italian') },
  { code: 'pt-PT', name: t('Portuguese (Portugal)') },
  { code: 'pt-BR', name: t('Portuguese (Brazil)') },
  { code: 'nl', name: t('Dutch') },
  { code: 'sv', name: t('Swedish') },
  { code: 'da', name: t('Danish') },
  { code: 'fi', name: t('Finnish') },
  { code: 'no', name: t('Norwegian') },
  { code: 'pl', name: t('Polish') },
  { code: 'cs', name: t('Czech') },
  { code: 'sk', name: t('Slovak') },
  { code: 'sl', name: t('Slovenian') },
  { code: 'hu', name: t('Hungarian') },
  { code: 'ro', name: t('Romanian') },
  { code: 'bg', name: t('Bulgarian') },
  { code: 'hr', name: t('Croatian') },
  { code: 'sr', name: t('Serbian') },
  { code: 'bs', name: t('Bosnian') },
  { code: 'uk', name: t('Ukrainian') },
  { code: 'el', name: t('Greek') },
  { code: 'tr', name: t('Turkish') },
  { code: 'ru', name: t('Russian') },
  { code: 'ar', name: t('Arabic') },
  { code: 'he', name: t('Hebrew') },
  { code: 'ja', name: t('Japanese') },
  { code: 'ko', name: t('Korean') },
  { code: 'zh-CN', name: t('Chinese (Simplified)') },
  { code: 'zh-TW', name: t('Chinese (Traditional)') },
  { code: 'hi', name: t('Hindi') },
  { code: 'fa', name: t('Persian') },
  { code: 'id', name: t('Indonesian') },
  { code: 'th', name: t('Thai') },
  { code: 'vi', name: t('Vietnamese') },
  { code: 'ca', name: t('Catalan') },
]

export const languageName = (code: string) => SUBTITLE_LANGUAGES.find((l) => l.code === code)?.name ?? code

// Audio languages a release can be in, by the names the server uses for the
// "Preferred audio language" setting (English first). These are the values
// the server stores, so they stay in English.
export const AUDIO_LANGUAGES: string[] = [
  'English', 'Arabic', 'Bosnian', 'Bulgarian', 'Chinese', 'Croatian', 'Czech', 'Danish', 'Dutch', 'Finnish', 'French',
  'German', 'Greek', 'Hebrew', 'Hindi', 'Hungarian', 'Italian', 'Japanese', 'Korean', 'Nordic', 'Norwegian', 'Polish',
  'Portuguese', 'Romanian', 'Russian', 'Serbian', 'Slovenian', 'Spanish', 'Swedish', 'Tamil', 'Telugu', 'Thai', 'Turkish',
  'Ukrainian', 'Vietnamese',
]
