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
  blurb: "TMDB supplies every poster, title, description and episode list. It's free.",
  without: 'searching, Discover, adding movies and shows, the calendar and importing an existing library won\'t work.',
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
  blurb: 'OpenSubtitles is where subtitles come from. The API key lets Mediarium search it, and a free account raises your daily download limit.',
  without: 'no subtitles are downloaded. Everything else works.',
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
      Trakt is a free site where people publish lists ("Best sci-fi of the 2010s", "Oscar winners"). A Client ID lets you import any <em>public</em> list into Discover and add everything on it in one go.
    </>
  ),
  without: 'you can\'t import Trakt lists on Discover.',
  steps: [
    <>Sign in or sign up at {link('https://trakt.tv', 'trakt.tv')} (it's free).</>,
    <>Open the {link('https://trakt.tv/oauth/applications', 'Trakt developer page')} and choose <strong>My Apps</strong>. If it asks, press <strong>Connect GitHub</strong> once (Trakt only reads your public GitHub name).</>,
    <>Press <strong>Create app</strong>, name it "Mediarium" and, if asked, set <strong>Redirect URI</strong> to <code>urn:ietf:wg:oauth:2.0:oob</code>. Leave the rest empty and save.</>,
    <>Copy the <strong>Client ID</strong> (not the secret) and paste it below.</>,
  ],
  fieldLabel: 'Trakt client ID',
  placeholder: 'Client ID',
}
