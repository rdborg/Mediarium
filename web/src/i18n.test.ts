// Run with: npm test (uses node's built-in test runner, no extra packages).
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { plural, preferredLanguage, setCatalogue, st, t } from './i18n.ts'

test('a stored choice wins, then the first browser language we have', () => {
  assert.equal(preferredLanguage('de', ['fr-FR']), 'de')
  assert.equal(preferredLanguage(null, ['sv-SE', 'fr-CA', 'es']), 'fr')
  assert.equal(preferredLanguage(null, ['it-IT', 'fr']), 'it')
  assert.equal(preferredLanguage(null, ['uk-UA']), 'uk')
  assert.equal(preferredLanguage(null, ['zh-Hans-CN']), 'zh-CN')
  assert.equal(preferredLanguage(null, ['zh-TW', 'en']), 'zh-CN')
  assert.equal(preferredLanguage('xx', ['pt-BR']), 'pt')
  assert.equal(preferredLanguage(null, ['pt-PT', 'en']), 'pt-PT')
  assert.equal(preferredLanguage(null, ['pt']), 'pt')
  assert.equal(preferredLanguage('pt-PT', ['en']), 'pt-PT')
  assert.equal(preferredLanguage(null, ['sv-SE', 'da']), 'en')
  assert.equal(preferredLanguage(null, []), 'en')
})

test('untranslated phrases fall back to the English', () => {
  setCatalogue('es', { Save: 'Guardar' })
  assert.equal(t('Save'), 'Guardar')
  assert.equal(t('Cancel'), 'Cancel')
  setCatalogue('en', {})
})

test('names in braces are filled in, unknown ones left alone', () => {
  setCatalogue('de', { '{name} was added': '{name} wurde hinzugefügt' })
  assert.equal(t('{name} was added', { name: 'Arrival' }), 'Arrival wurde hinzugefügt')
  assert.equal(t('Keep {this}', { name: 'x' }), 'Keep {this}')
  setCatalogue('en', {})
})

test('plural picks the form the language uses', () => {
  setCatalogue('en', {})
  assert.equal(plural(1, '{n} movie', '{n} movies'), '1 movie')
  assert.equal(plural(0, '{n} movie', '{n} movies'), '0 movies')
  setCatalogue('fr', { '{n} movie': '{n} film', '{n} movies': '{n} films' })
  assert.equal(plural(0, '{n} movie', '{n} movies'), '0 film')
  assert.equal(plural(2, '{n} movie', '{n} movies'), '2 films')
  setCatalogue('en', {})
})

test('Russian, Ukrainian and Polish have a third plural form', () => {
  const entries = { '{n} movie': '{n} фильм', '{n} movies': '{n} фильмов', '{n} movies|few': '{n} фильма' }
  setCatalogue('ru', entries)
  assert.equal(plural(1, '{n} movie', '{n} movies'), '1 фильм')
  assert.equal(plural(21, '{n} movie', '{n} movies'), '21 фильм')
  assert.equal(plural(2, '{n} movie', '{n} movies'), '2 фильма')
  assert.equal(plural(24, '{n} movie', '{n} movies'), '24 фильма')
  assert.equal(plural(5, '{n} movie', '{n} movies'), '5 фильмов')
  assert.equal(plural(11, '{n} movie', '{n} movies'), '11 фильмов')
  assert.equal(plural(0, '{n} movie', '{n} movies'), '0 фильмов')
  setCatalogue('pl', { '{n} movie': '{n} film', '{n} movies': '{n} filmów', '{n} movies|few': '{n} filmy' })
  assert.equal(plural(3, '{n} movie', '{n} movies'), '3 filmy')
  assert.equal(plural(5, '{n} movie', '{n} movies'), '5 filmów')
  setCatalogue('en', {})
})

test('a sentence written for exactly one is used for 1 only', () => {
  const one = 'Remove this download from the list?'
  const other = 'Remove {n} downloads from the list?'
  setCatalogue('ru', { [one]: 'Убрать эту загрузку?', [other]: 'Убрать {n} загрузок?', [`${other}|few`]: 'Убрать {n} загрузки?' })
  assert.equal(plural(1, one, other), 'Убрать эту загрузку?')
  assert.equal(plural(21, one, other), 'Убрать 21 загрузок?')
  assert.equal(plural(22, one, other), 'Убрать 22 загрузки?')
  setCatalogue('uk', { [one]: 'Прибрати це завантаження?', [other]: 'Прибрати {n} завантажень?', [`${other}|one`]: 'Прибрати {n} завантаження?' })
  assert.equal(plural(101, one, other), 'Прибрати 101 завантаження?')
  setCatalogue('fr', { [one]: 'Retirer ce téléchargement ?', [other]: 'Retirer {n} téléchargements ?' })
  assert.equal(plural(0, one, other), 'Retirer 0 téléchargements ?')
  assert.equal(plural(1, one, other), 'Retirer ce téléchargement ?')
  setCatalogue('en', {})
  assert.equal(plural(1, one, other), one)
  assert.equal(plural(3, one, other), 'Remove 3 downloads from the list?')
})

test('a very long server text is not matched and comes back quickly', () => {
  const key = 'Mediarium {0} was installed by {1} ({2}, checksum {3}). It restarts to use it.'
  setCatalogue('ru', { [key]: 'Mediarium {0} установлен {1} ({2}, контрольная сумма {3}). Он перезапустится.' })
  const long = 'Mediarium ' + 'a ('.repeat(9500) + 'x'
  assert.ok(long.length > 28000)
  const started = Date.now()
  assert.equal(st(long), long)
  assert.ok(Date.now() - started < 200, 'st() took too long')
  // A near miss just under the cap (right start and end, no checksum) is still quick.
  const near = 'Mediarium ' + 'was installed by ('.repeat(25) + '). It restarts to use it.'
  assert.ok(near.length <= 500)
  const again = Date.now()
  assert.equal(st(near), near)
  assert.ok(Date.now() - again < 200, 'st() took too long')
  assert.equal(st('Mediarium 3.1 was installed by Ann (amd64, checksum ok). It restarts to use it.'), 'Mediarium 3.1 установлен Ann (amd64, контрольная сумма ok). Он перезапустится.')
  setCatalogue('en', {})
})

test('a sentence the server sent is translated, with its pieces put back', () => {
  setCatalogue('de', {
    'That quality is not allowed.': 'Diese Qualität ist nicht erlaubt.',
    'Downloads at the same time must be a number from {0} to {1}.': 'Gleichzeitige Downloads müssen eine Zahl von {0} bis {1} sein.',
    'The name says episode {0}, but you chose episode {1}.': 'Der Name sagt Folge {0}, aber du hast Folge {1} gewählt.',
    "Can't reach {0}.": '{0} ist nicht erreichbar.',
    "Can't reach {0}: {1}": '{0} ist nicht erreichbar: {1}',
  })
  assert.equal(st('That quality is not allowed.'), 'Diese Qualität ist nicht erlaubt.')
  assert.equal(st('Downloads at the same time must be a number from 1 to 5.'), 'Gleichzeitige Downloads müssen eine Zahl von 1 bis 5 sein.')
  assert.equal(st('The name says episode 3, but you chose episode 4.'), 'Der Name sagt Folge 3, aber du hast Folge 4 gewählt.')
  assert.equal(st("Can't reach plex: connection refused"), 'plex ist nicht erreichbar: connection refused')
  assert.equal(st('Something nobody translated.'), 'Something nobody translated.')
  assert.equal(st(''), '')
  assert.equal(st(undefined), '')
  setCatalogue('en', {})
  assert.equal(st('That quality is not allowed.'), 'That quality is not allowed.')
})
