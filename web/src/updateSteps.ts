// The plain-language pieces of the update card: the steps for each way of
// running Mediarium, and the short sentences about where the running program
// came from. No React in here so they can be tested.

export interface Steps {
  title: string
  steps: string[]
}

// Steps for updating the way the person installed Mediarium. Docker installs
// get Synology Container Manager, Unraid and plain Compose; the -full image gets
// one extra line about keeping its tag.
export function installSteps(install: { kind: 'docker' | 'native'; full: boolean }): { groups: Steps[]; notes: string[] } {
  if (install.kind === 'native') {
    return {
      groups: [
        {
          title: 'Update Mediarium',
          steps: [
            'Download a backup first (Settings, System).',
            'Download the new version from the release page and unpack it.',
            'Stop Mediarium, replace the program file with the new one, and start it again.',
          ],
        },
      ],
      notes: ['Your settings and library are kept. Mediarium updates its database when it starts.'],
    }
  }
  const tag = install.full ? 'latest-full' : 'latest'
  const notes = ['Your settings and library are kept. Mediarium updates its database when it starts. Download a backup first (Settings, System).']
  if (install.full) {
    notes.push(`You use the image with the Cloudflare helper built in. Keep its tag ending in -full, for example ghcr.io/rdborg/mediarium:${tag}.`)
  }
  return {
    groups: [
      {
        title: 'Synology Container Manager',
        steps: [
          'Open Container Manager and go to Project.',
          'Select mediarium, then choose Action, Stop, and then Action, Clean. This removes the container, not your folders.',
          'Go to Image, select ghcr.io/rdborg/mediarium and delete it. If it says Update available, use that instead and skip the Build below.',
          'Go back to Project, select mediarium and choose Action, Build. Container Manager downloads the new version and starts it.',
        ],
      },
      {
        title: 'Unraid',
        steps: ['Open the Docker tab and click Check for Updates.', 'When Mediarium says update ready, click it and choose Update.'],
      },
      {
        title: 'Docker Compose',
        steps: ['In the folder with your compose file, run: docker compose pull', 'Then run: docker compose up -d'],
      },
    ],
    notes,
  }
}

// Where the running program comes from, for the Updates card.
export function programSummary(s: { running: string; image: string; pushed: { version: string; running: boolean } | null }): string {
  if (!s.image) return `You are running Mediarium ${s.running}.`
  if (s.pushed && s.pushed.running) {
    return `You are running Mediarium ${s.running}, installed on top of the Docker image, which has ${s.image}.`
  }
  if (s.pushed) {
    return `You are running Mediarium ${s.running}. An installed update (${s.pushed.version}) is waiting for the next restart. The Docker image has ${s.image}.`
  }
  return `You are running Mediarium ${s.running}.`
}

// The sentence about the last check.
export function checkedText(n: { enabled: boolean; checkedAt?: string; error?: string }, ago: (iso: string | undefined) => string): string {
  if (n.error) return n.checkedAt ? `${n.error} The last good check was ${ago(n.checkedAt)}.` : n.error
  if (n.checkedAt) return `Checked ${ago(n.checkedAt)}.`
  return n.enabled ? 'Not checked yet.' : 'Checking is switched off. Press Check now to look.'
}

// How the page waits for Mediarium to come back after it restarts.
export interface WaitOptions {
  expectVersion?: string
  fetchVersion: () => Promise<string | null> // the running version, null while it does not answer
  sleep: (ms: number) => Promise<void>
  now: () => number
  minMs?: number // do not believe an answer before this: the old process may still be up
  maxMs?: number
  everyMs?: number
}

// Resolves 'back' once the app answers (with the expected version, if one is
// given), or 'timeout'.
export async function waitForApp(o: WaitOptions): Promise<'back' | 'timeout'> {
  const start = o.now()
  const minMs = o.minMs ?? 4000
  const maxMs = o.maxMs ?? 150000
  const every = o.everyMs ?? 2000
  for (;;) {
    await o.sleep(every)
    const elapsed = o.now() - start
    if (elapsed > maxMs) return 'timeout'
    const v = await o.fetchVersion()
    if (v !== null && elapsed >= minMs && (!o.expectVersion || v === o.expectVersion)) return 'back'
  }
}
