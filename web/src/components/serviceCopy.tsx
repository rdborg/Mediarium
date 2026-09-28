import type { ReactNode } from 'react'

// The wording for each third-party service, shared by Settings and the
// first-run wizard so both explain the same thing the same way.
export interface ServiceCopy {
  title: string
  required: boolean
  blurb: ReactNode
  without: ReactNode
  steps: ReactNode[]
  fieldLabel: string
  placeholder: string
}

const link = (href: string, text: string) => (
  <a href={href} target="_blank" rel="noreferrer">
    {text}
  </a>
)

export const TMDB_COPY: ServiceCopy = {
  title: 'TMDB (movie and show information)',
  required: true,
  blurb: 'TMDB provides every poster, title, description and episode list in Mediarium. It is free.',
  without: 'searching, Discover, adding movies and shows, the calendar and importing an existing library will not work.',
  steps: [
    <>Create a free account at {link('https://www.themoviedb.org/signup', 'themoviedb.org')}.</>,
    <>Open {link('https://www.themoviedb.org/settings/api', 'Settings > API')} and request an API key (choose "Developer" and describe it as personal use).</>,
    <>Copy the <strong>API Key</strong> (the shorter v3 one) and paste it below.</>,
  ],
  fieldLabel: 'TMDB API key',
  placeholder: 'v3 API key',
}

export const OPENSUBTITLES_COPY: ServiceCopy = {
  title: 'OpenSubtitles (subtitles)',
  required: false,
  blurb: 'OpenSubtitles is where Mediarium finds subtitles. The API key lets it search; a free account (below) raises how many you can download each day.',
  without: 'no subtitles will be downloaded. Movies and shows still download and import normally.',
  steps: [
    <>Create a free account at {link('https://www.opensubtitles.com/en/newuser', 'opensubtitles.com')}.</>,
    <>Open {link('https://www.opensubtitles.com/en/consumers', 'your API consumers page')} and create a consumer named "Mediarium".</>,
    <>Copy its <strong>API key</strong> and paste it below.</>,
  ],
  fieldLabel: 'OpenSubtitles API key',
  placeholder: 'API key',
}

export const TRAKT_COPY: ServiceCopy = {
  title: 'Trakt (list import)',
  required: false,
  blurb: (
    <>
      Trakt is a free website where people track what they watch and publish lists ("Best sci-fi of the 2010s", "Oscar winners").
      Mediarium can import any <em>public</em> Trakt list into Discover so you can add everything on it in one go. Trakt asks every
      app that reads its lists to identify itself with a Client ID.
    </>
  ),
  without: 'you can not import Trakt lists on the Discover page. Nothing else depends on it.',
  steps: [
    <>Sign in or sign up at {link('https://trakt.tv', 'trakt.tv')} (it is free; signing in with a linked account such as GitHub is just one way to make one).</>,
    <>Go to {link('https://trakt.tv/oauth/applications/new', 'Your API Apps > New Application')}.</>,
    <>Name it "Mediarium" and set <strong>Redirect URI</strong> to <code>urn:ietf:wg:oauth:2.0:oob</code>. Leave the rest empty and save.</>,
    <>Copy the <strong>Client ID</strong> (not the secret) and paste it below.</>,
  ],
  fieldLabel: 'Trakt client ID',
  placeholder: 'Client ID',
}
