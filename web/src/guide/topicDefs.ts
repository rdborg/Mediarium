// The topics of the in-app user guide, and which documentation pages feed each.
//
// The pages in docs/ are the source of truth. web/tools/build-guide.mjs reads
// them with these rules while the web app is built and writes the bundle the
// guide loads. A topic can use whole pages or only some sections of a page
// (INSTALL.md is split across several topics). The build stops with a clear
// message when a page or a named section no longer exists, so a renamed
// heading in the docs cannot silently empty a topic.
//
// The guide is in English only for now, so these texts are not translated.

export interface SourceDef {
  // The file in docs/.
  file: string
  // Gives this page its own heading inside a topic that has several pages.
  // The page's headings then sit one level below it.
  title?: string
  // Keep only these "## " sections (and what is under them).
  only?: string[]
  // Keep everything except these "## " sections.
  except?: string[]
}

export interface TopicDef {
  id: string
  title: string
  // One line under the title in the topic list.
  summary: string
  group: string
  sources: SourceDef[]
}

export const GROUPS = ['Start here', 'Your media', 'Downloading', 'People and apps', 'Looking after it', 'Installing'] as const

// Notes for people who script against the API and similar developer parts are
// left out of the guide; they stay in the full documentation.
export const DROP_HEADINGS = /^(for scripts|for developers|scripts|api|contents)$/i

export const TOPICS: TopicDef[] = [
  {
    id: 'getting-started',
    title: 'Getting started',
    summary: 'From a fresh install to your first movie: the setup wizard, media types and languages.',
    group: 'Start here',
    sources: [
      { file: 'INSTALL.md', only: ['Quick start', 'The setup wizard'] },
      { file: 'modules.md', title: 'Media types' },
      { file: 'lifecycle.md', title: 'Library lifecycle' },
      { file: 'languages.md', title: 'Languages' },
    ],
  },
  {
    id: 'adding',
    title: 'Adding movies and shows',
    summary: 'Discover, the Add dialog, import lists, a library you already have, and moving over from other apps.',
    group: 'Start here',
    sources: [
      { file: 'discover.md' },
      { file: 'import-library.md', title: 'Importing a library you already have' },
      { file: 'lists.md', title: 'Import lists' },
      { file: 'migrate.md', title: 'Moving from Radarr, Sonarr and other apps' },
    ],
  },
  {
    id: 'library',
    title: 'Your library',
    summary: 'Finding, selecting and changing many titles at once, tags, folders, file names and collections.',
    group: 'Your media',
    sources: [{ file: 'library.md' }],
  },
  {
    id: 'music',
    title: 'Music',
    summary: 'Artists and albums, audio quality, the built-in player, playlists and importing a collection.',
    group: 'Your media',
    sources: [{ file: 'music.md' }],
  },
  {
    id: 'books',
    title: 'Ebooks and audiobooks',
    summary: 'Adding books, which release is picked, reading and listening, and e-readers.',
    group: 'Your media',
    sources: [{ file: 'books.md' }],
  },
  {
    id: 'archiver',
    title: 'Channel archiver',
    summary: 'Following YouTube channels with a yt-dlp you supply, what the warning means and where files go.',
    group: 'Your media',
    sources: [{ file: 'archiver.md' }],
  },
  {
    id: 'quality',
    title: 'Quality profiles',
    summary: 'What a profile decides, the built-in presets, cutoffs, upgrades, size limits and custom formats.',
    group: 'Your media',
    sources: [{ file: 'quality-profiles.md' }],
  },
  {
    id: 'subtitles',
    title: 'Subtitles',
    summary: 'Where subtitles come from, the kinds, daily limits, and fixing one that is out of time.',
    group: 'Your media',
    sources: [{ file: 'subtitles.md' }],
  },
  {
    id: 'downloads',
    title: 'Downloads: Usenet, torrents and VPN',
    summary: 'The built-in downloaders, download clients, the torrent port, seeding, clean-up and the VPN.',
    group: 'Downloading',
    sources: [{ file: 'downloads.md' }, { file: 'vpn.md', title: 'VPN protection' }, { file: 'cross-seeding.md', title: 'Cross-seeding' }],
  },
  {
    id: 'indexers',
    title: 'Indexers and searching',
    summary: 'Adding Usenet and torrent indexers, sites behind Cloudflare, and grabbing from IRC announcements.',
    group: 'Downloading',
    sources: [{ file: 'indexers.md' }, { file: 'irc-announce.md', title: 'Grabbing from IRC announcements' }],
  },
  {
    id: 'activity',
    title: 'Activity and the queue',
    summary: 'The queue, history, why a title has not downloaded, pausing, retrying and the recycle bin.',
    group: 'Downloading',
    sources: [{ file: 'activity.md' }],
  },
  {
    id: 'accounts',
    title: 'Accounts and requests',
    summary: 'Family accounts, what each role can do, requests, two-step sign-in, single sign-on and passkeys.',
    group: 'People and apps',
    sources: [{ file: 'accounts.md' }],
  },
  {
    id: 'media-servers',
    title: 'Media servers and apps',
    summary: 'Plex, Jellyfin, Emby, Silo and others; Overseerr and phone apps; chat and voice assistants; a script after an import.',
    group: 'People and apps',
    sources: [
      { file: 'media-servers.md' },
      { file: 'other-apps.md', title: 'Overseerr, dashboards and phone apps' },
      { file: 'download-apps.md', title: 'Watching downloads from your phone (nzb360)' },
      { file: 'mcp.md', title: 'Chat programs and voice assistants (MCP)' },
      { file: 'scripts.md', title: 'Running a script after each import' },
    ],
  },
  {
    id: 'notifications',
    title: 'Notifications',
    summary: 'Email, ntfy, Discord, Telegram and more: setting one up, testing it and what a message says.',
    group: 'People and apps',
    sources: [{ file: 'notifications.md' }],
  },
  {
    id: 'backups-updates',
    title: 'Backups and updates',
    summary: 'Updating from inside the app or the command line, backing up and restoring, cloud backups, settings in a file.',
    group: 'Looking after it',
    sources: [
      { file: 'INSTALL.md', only: ['Updating', 'Updating without rebuilding the container', 'Backup and restore'] },
      { file: 'config-file.md', title: 'Keeping your settings in a file' },
      { file: 'config-as-code.md', title: 'Settings file with a check' },
    ],
  },
  {
    id: 'problems',
    title: 'Fixing problems',
    summary: 'Reading Logs and errors, what the usual messages mean, and sending a report that helps.',
    group: 'Looking after it',
    sources: [
      { file: 'logs-and-errors.md' },
      { file: 'report-a-problem.md', title: 'Report a problem or share an idea' },
      { file: 'INSTALL.md', title: 'Install and container problems', only: ['Troubleshooting'] },
    ],
  },
  {
    id: 'usage-stats',
    title: 'Anonymous usage numbers',
    summary: 'The optional message that helps the project see which devices are popular: what is in it, where it goes, and how to say no.',
    group: 'Looking after it',
    sources: [{ file: 'usage-stats.md' }],
  },
  {
    id: 'artwork',
    title: 'Artwork and privacy',
    summary: 'Where posters and backdrops come from, why your browsers no longer contact TMDB, the sizes, the cache and the switch.',
    group: 'Looking after it',
    sources: [{ file: 'artwork.md' }],
  },
  {
    id: 'foldable-phones',
    title: 'Foldable and dual-screen phones',
    summary: 'Using the whole screen of a foldable phone: the top and side strips, the Automatic setting and how to tune it.',
    group: 'Looking after it',
    sources: [{ file: 'foldable-phones.md' }],
  },
  {
    id: 'internet',
    title: 'Using it from the internet',
    summary: 'A reverse proxy checklist, trusted proxies and what Mediarium does to protect itself.',
    group: 'Looking after it',
    sources: [{ file: 'security.md' }],
  },
  {
    id: 'install-docker',
    title: 'Install with Docker',
    summary: 'The compose file, folders, hardlinks, ports, environment variables and everyday commands.',
    group: 'Installing',
    sources: [
      {
        file: 'INSTALL.md',
        except: ['Quick start', 'The setup wizard', 'Updating', 'Updating without rebuilding the container', 'Backup and restore', 'Troubleshooting', 'Building from source'],
      },
      { file: 'PLATFORMS.md', title: 'What runs where' },
    ],
  },
  {
    id: 'install-nas',
    title: 'Install on Synology, Unraid, QNAP or Linux',
    summary: 'Step-by-step guides for a NAS or a plain Linux server.',
    group: 'Installing',
    sources: [
      { file: 'synology.md', title: 'Synology' },
      { file: 'unraid.md', title: 'Unraid' },
      { file: 'qnap.md', title: 'QNAP' },
      { file: 'linux.md', title: 'Linux' },
    ],
  },
]
