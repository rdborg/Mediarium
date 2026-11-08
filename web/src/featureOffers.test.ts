// Run with: npm test
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MODULES_BEFORE_OFFERS, OFFERS, offersAfterUpdate, offersInSettings, type OfferId } from './featureOffers.ts'
import { MODULE_KEYS } from './moduleKeys.ts'

test('every module added since the offers began has an offer, and every offer is sound', () => {
  const ids = new Set<string>()
  for (const o of OFFERS) {
    assert.ok(!ids.has(o.id), `${o.id} twice`)
    ids.add(o.id)
    assert.match(o.since, /^\d+\.\d+\.\d+$/, o.id)
    assert.ok(o.title && o.text, o.id)
  }
  for (const key of MODULE_KEYS) {
    if (MODULES_BEFORE_OFFERS.includes(key)) continue
    assert.ok(
      OFFERS.some((o) => o.action.kind === 'module' && o.action.module === key && o.wizard !== ''),
      `the ${key} module needs an offer that the setup wizard has too`,
    )
  }
})

test('after an update only what is new, off and not answered is offered', () => {
  const off = () => false
  const ids = (list: { id: OfferId }[]) => list.map((o) => o.id).sort()
  assert.deepEqual(offersAfterUpdate(null, '4.0.0', {}, off), [], 'a new install had the wizard')
  assert.deepEqual(ids(offersAfterUpdate('3.9.2', '4.0.0', {}, off)), ['anime', 'archiver', 'lifecycle', 'omdb', 'prowlarr', 'rss-lists', 'soulseek', 'thetvdb', 'torrent-route'])
  assert.deepEqual(ids(offersAfterUpdate('3.9.0', '3.9.1', {}, off)), ['udp-dht'])
  assert.deepEqual(ids(offersAfterUpdate('3.9.2', '4.0.0', { anime: 'never', thetvdb: 'later:4.0.0' }, off)), ['archiver', 'lifecycle', 'omdb', 'prowlarr', 'rss-lists', 'soulseek', 'torrent-route'])
  assert.deepEqual(ids(offersAfterUpdate('3.9.2', '4.0.1', { thetvdb: 'later:4.0.0' }, off)), ['anime', 'archiver', 'lifecycle', 'omdb', 'prowlarr', 'rss-lists', 'soulseek', 'thetvdb', 'torrent-route'], 'not now asks again after the next update')
  assert.deepEqual(ids(offersAfterUpdate('3.9.2', '4.0.0', {}, (id) => id === 'anime')), ['archiver', 'lifecycle', 'omdb', 'prowlarr', 'rss-lists', 'soulseek', 'thetvdb', 'torrent-route'], 'one that is on already is not offered')
  assert.ok(offersInSettings('4.0.0', off).some((o) => o.id === 'anime'), 'Settings lists a Never too, so it can be taken back')
})
