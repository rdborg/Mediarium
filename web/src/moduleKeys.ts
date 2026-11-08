// Every module key at run time. The Record makes TypeScript refuse to build
// when a new ModuleKey is not listed here, so the test that every new module
// has a "switch this on?" offer (featureOffers.test.ts) can't miss one.
import type { ModuleKey } from './api.ts'

const ALL: Record<ModuleKey, true> = { movies: true, tv: true, music: true, ebooks: true, audiobooks: true, anime: true, lifecycle: true, archiver: true }

export const MODULE_KEYS = Object.keys(ALL) as ModuleKey[]
