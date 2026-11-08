import { lazy, Suspense, useEffect, type ComponentType, type ReactNode } from 'react'
import { Navigate, Route, BrowserRouter as Router, Routes, useLocation } from 'react-router-dom'
import { isAdmin } from './api'
import GuidePopupHost from './components/GuidePopup'
import { BASE_PATH } from './base'
import { t } from './i18n'
import { AuthProvider, useAuth } from './AuthContext'
import { ModulesProvider } from './ModulesContext'
import AppShell from './components/AppShell'
import TwoPane from './components/TwoPane'
import ErrorBoundary from './components/ErrorBoundary'
import Dashboard from './pages/Dashboard'
import Splash from './components/Splash'
import Login from './pages/Login'
import MusicAlbumRedirect from './pages/MusicAlbumRedirect'
import NotFound from './pages/NotFound'
import SettingsLayout from './pages/settings/SettingsLayout'
import { SETTINGS_REDIRECTS } from './settingsNav'

// Mediarium Books (reader and player), an app of its own, loaded when opened.
import MusicPopout from './components/MusicPopout'
const BookshelfApp = lazy(() => import('./bookshelf/BookshelfApp'))

// The main pages are fetched as they are opened, not with the first load, so the
// page that opens first is ready sooner (the rest is a big part of the program).
// Once the app is up and the browser is idle they are all fetched in the
// background, so opening one later is as quick as if it had been there all along.
// (Moving between pages keeps the old page on screen until the new one is ready,
// so nothing flashes.)
const toFetchLater: (() => Promise<unknown>)[] = []
function page<P>(load: () => Promise<{ default: ComponentType<P> }>) {
  toFetchLater.push(load)
  return lazy(load)
}
const Upcoming = page(() => import('./pages/Upcoming'))
const Discover = page(() => import('./pages/Discover'))
const DiscoverAll = page(() => import('./pages/DiscoverAll'))
const Library = page(() => import('./pages/Library'))
const MovieDetail = page(() => import('./pages/MovieDetail'))
const Queue = page(() => import('./pages/Queue'))
const MusicDiscoverAll = page(() => import('./pages/MusicDiscoverAll'))
const MusicArtist = page(() => import('./pages/MusicArtist'))
const BookDetail = page(() => import('./pages/BookDetail'))
const BookWork = page(() => import('./pages/BookWork'))
const MusicRelease = page(() => import('./pages/MusicRelease'))
const Search = page(() => import('./pages/Search'))
const SeriesDetail = page(() => import('./pages/SeriesDetail'))
const ShowDetail = page(() => import('./pages/ShowDetail'))
const Person = page(() => import('./pages/Person'))

// Fetches the pages above one after another once the browser has nothing else to do.
function fetchPagesWhenIdle() {
  const later = (fn: () => void) => (typeof window.requestIdleCallback === 'function' ? window.requestIdleCallback(fn, { timeout: 4000 }) : window.setTimeout(fn, 1500))
  let i = 0
  const next = () => {
    const load = toFetchLater[i++]
    if (!load) return
    load().catch(() => undefined).finally(() => later(next))
  }
  later(next)
}

// Pages most people open now and then are fetched when first opened, so the first load is smaller.
const Onboarding = lazy(() => import('./pages/Onboarding'))
const Profile = lazy(() => import('./pages/Profile'))
const ImportLibrary = lazy(() => import('./pages/ImportLibrary'))
const MusicImport = lazy(() => import('./pages/MusicImport'))
const MusicPlaylists = lazy(() => import('./pages/MusicPlaylists').then((m) => ({ default: m.MusicPlaylists })))
const MusicPlaylistPage = lazy(() => import('./pages/MusicPlaylists').then((m) => ({ default: m.MusicPlaylistPage })))
const Statistics = lazy(() => import('./pages/Statistics'))
const ManualImport = lazy(() => import('./pages/ManualImport'))
const ReleaseSearch = lazy(() => import('./pages/ReleaseSearch'))
const About = lazy(() => import('./pages/About'))
const DownloadRulesSettings = lazy(() => import('./pages/settings/DownloadSettings'))
const UsenetSettings = lazy(() => import('./pages/settings/DownloadSettings').then((m) => ({ default: m.UsenetSettings })))
const TorrentSettings = lazy(() => import('./pages/settings/DownloadSettings').then((m) => ({ default: m.TorrentSettings })))
const NamingSettings = lazy(() => import('./pages/settings/MediaSettings').then((m) => ({ default: m.NamingSettings })))
const GeneralSettings = lazy(() => import('./pages/settings/GeneralSettings'))
const SecuritySettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.SecuritySettings })))
const ServicesSettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.ServicesSettings })))
const RequestsSettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.RequestsSettings })))
const SignInSettings = lazy(() => import('./pages/Profile').then((m) => ({ default: m.SignInSettings })))
const Guide = lazy(() => import('./pages/Guide'))
const SystemSettings = lazy(() => import('./pages/settings/SystemSettings'))
const BackupSettings = lazy(() => import('./pages/settings/BackupSettings'))
const AllAccounts = lazy(() => import('./pages/Profile').then((m) => ({ default: m.AllAccounts })))
const AutomationSettings = lazy(() => import('./pages/settings/QualitySettings').then((m) => ({ default: m.AutomationSettings })))
const LogsSettings = lazy(() => import('./pages/settings/LogsSettings'))
const IndexerSettings = lazy(() => import('./pages/settings/IndexerSettings'))
const MediaSettings = lazy(() => import('./pages/settings/MediaSettings'))
const ModulesSettings = lazy(() => import('./pages/settings/ModulesSettings'))
const MetadataSettings = lazy(() => import('./pages/settings/MetadataSettings'))
const ListsSettings = lazy(() => import('./pages/settings/ListsSettings'))
const MediaServerSettings = lazy(() => import('./pages/settings/MediaServerSettings'))
const OtherAppsSettings = lazy(() => import('./pages/settings/OtherAppsSettings'))
const AppLinksSettings = lazy(() => import('./pages/settings/AppLinksSettings'))
const AppFrame = lazy(() => import('./pages/AppFrame'))
const MigrateSettings = lazy(() => import('./pages/settings/MigrateSettings'))
const NotificationSettings = lazy(() => import('./pages/settings/NotificationSettings'))
const QualitySettings = lazy(() => import('./pages/settings/QualitySettings'))
const SubtitleSettings = lazy(() => import('./pages/settings/SubtitleSettings'))
const VPNSettings = lazy(() => import('./pages/settings/VPNSettings'))
const ConnectApp = lazy(() => import('./pages/ConnectApp'))

// Pages only an administrator can use. A member who opens one (for example
// by typing its address) is sent somewhere they can use instead.
function AdminOnly({ children, fallback = '/settings/profile' }: { children: ReactNode; fallback?: string }) {
  const { user } = useAuth()
  return isAdmin(user) ? <>{children}</> : <Navigate to={fallback} replace />
}

function Gate() {
  const { loading, offline, slow, firstRunNeeded, user, needsWizard, refresh } = useAuth()
  const signedIn = !!user
  useEffect(() => {
    if (signedIn) fetchPagesWhenIdle()
  }, [signedIn])

  if (loading) {
    if (slow) {
      return (
        <Splash
          label={t('Mediarium is slow to answer')}
          hint={t('It may be busy with a download or an import. It keeps trying by itself.')}
          onRetry={() => void refresh()}
        />
      )
    }
    return <Splash label={offline ? t('Reconnecting to Mediarium') : t('Loading')} />
  }
  if (firstRunNeeded) {
    return (
      <Suspense fallback={<Splash label={t('Loading')} />}>
        <Onboarding />
      </Suspense>
    )
  }
  if (!user) {
    return <Login />
  }
  if (needsWizard) {
    return (
      <Suspense fallback={<Splash label={t('Loading')} />}>
        <Onboarding startStep={1} />
      </Suspense>
    )
  }

  return (
    <>
    <Routes>
      <Route
        path="/bookshelf/*"
        element={
          <Suspense fallback={<Splash label={t('Loading')} />}>
            <BookshelfApp />
          </Suspense>
        }
      />
      <Route path="/music-player" element={<MusicPopout />} />
      <Route
        path="/connect-app"
        element={
          <Suspense fallback={<Splash label={t('Loading')} />}>
            <ConnectApp />
          </Suspense>
        }
      />
      <Route element={<AppShell />}>
        <Route index element={<Dashboard />} />
        <Route path="/search" element={<Search />} />
        <Route path="/search/releases" element={<ReleaseSearch />} />
        <Route path="/stats" element={<Navigate to="/settings/statistics" replace />} />
        <Route path="/import/manual" element={<AdminOnly><ManualImport /></AdminOnly>} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/discover/all" element={<DiscoverAll />} />
        <Route path="/discover/music" element={<MusicDiscoverAll />} />
        {/* The Library and the pages opened from it share one layout: on a tablet the list stays beside the open page. */}
        <Route element={<TwoPane list={(pane) => <Library pane={pane} />} />}>
          <Route path="/library" element={null} />
          <Route path="/title/:tmdbId" element={<MovieDetail />} />
          <Route path="/series/:id" element={<SeriesDetail />} />
          <Route path="/book/:id" element={<BookDetail />} />
          <Route path="/music/artist/:id" element={<MusicArtist />} />
        </Route>
        <Route path="/import" element={<AdminOnly fallback="/library"><ImportLibrary /></AdminOnly>} />
        <Route path="/music/playlists" element={<MusicPlaylists />} />
        <Route path="/music/playlists/:id" element={<MusicPlaylistPage />} />
        <Route path="/music/album/:id" element={<MusicAlbumRedirect />} />
        <Route path="/music/release/:mbid" element={<MusicRelease />} />
        <Route path="/books/work/:key" element={<BookWork />} />
        <Route path="/music/import" element={<AdminOnly fallback="/library"><MusicImport /></AdminOnly>} />
        <Route path="/show/:tmdbId" element={<ShowDetail />} />
        <Route path="/person/:id" element={<Person />} />
        <Route path="/wanted" element={<Upcoming tab="wanted" />} />
        <Route path="/calendar" element={<Upcoming tab="calendar" />} />
        <Route path="/queue" element={<Queue />} />
        <Route path="/apps/:id" element={<AppFrame />} />
        <Route path="/help" element={<Guide />} />
        <Route path="/help/:topic" element={<Guide />} />
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to="general" replace />} />
          <Route path="modules" element={<AdminOnly><ModulesSettings /></AdminOnly>} />
          <Route path="media" element={<AdminOnly><MediaSettings /></AdminOnly>} />
          <Route path="naming" element={<AdminOnly><NamingSettings /></AdminOnly>} />
          <Route path="quality" element={<AdminOnly><QualitySettings /></AdminOnly>} />
          <Route path="automation" element={<AdminOnly><AutomationSettings /></AdminOnly>} />
          <Route path="indexers" element={<AdminOnly><IndexerSettings /></AdminOnly>} />
          <Route path="usenet" element={<AdminOnly><UsenetSettings /></AdminOnly>} />
          <Route path="torrents" element={<AdminOnly><TorrentSettings /></AdminOnly>} />
          <Route path="download-rules" element={<AdminOnly><DownloadRulesSettings /></AdminOnly>} />
          <Route path="vpn" element={<AdminOnly><VPNSettings /></AdminOnly>} />
          <Route path="subtitles" element={<AdminOnly><SubtitleSettings /></AdminOnly>} />
          <Route path="metadata" element={<AdminOnly><MetadataSettings /></AdminOnly>} />
          <Route path="lists" element={<AdminOnly><ListsSettings /></AdminOnly>} />
          <Route path="media-servers" element={<AdminOnly><MediaServerSettings /></AdminOnly>} />
          <Route path="other-apps" element={<AdminOnly><OtherAppsSettings /></AdminOnly>} />
          <Route path="addons" element={<AdminOnly><AppLinksSettings /></AdminOnly>} />
          <Route path="migrate" element={<AdminOnly><MigrateSettings /></AdminOnly>} />
          <Route path="notifications" element={<AdminOnly><NotificationSettings /></AdminOnly>} />
          <Route path="profile" element={<Profile />} />
          <Route path="system" element={<AdminOnly><SystemSettings /></AdminOnly>} />
          <Route path="backup" element={<AdminOnly><BackupSettings /></AdminOnly>} />
          <Route path="accounts" element={<AdminOnly><AllAccounts /></AdminOnly>} />
          <Route path="requests" element={<AdminOnly><RequestsSettings /></AdminOnly>} />
          <Route path="sign-in" element={<AdminOnly><SignInSettings /></AdminOnly>} />
          <Route path="general" element={<GeneralSettings />} />
          <Route path="security" element={<SecuritySettings />} />
          <Route path="services" element={<ServicesSettings />} />
          <Route path="logs" element={<AdminOnly><LogsSettings /></AdminOnly>} />
          <Route path="about" element={<About />} />
          <Route path="statistics" element={<Statistics />} />
          {SETTINGS_REDIRECTS.map(([from, to]) => (
            <Route key={from} path={from.replace('/settings/', '')} element={<Navigate to={to} replace />} />
          ))}
        </Route>
        <Route path="/profile" element={<Navigate to="/settings/profile" replace />} />
        <Route path="/about" element={<Navigate to="/settings/about" replace />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
    <GuidePopupHost />
    </>
  )
}

// A page that crashes while it draws shows a short message instead of a blank
// screen, and going to another page clears it.
function Guarded({ children }: { children: ReactNode }) {
  return <ErrorBoundary resetKey={useLocation().pathname}>{children}</ErrorBoundary>
}

export default function App() {
  return (
    <Router basename={BASE_PATH || undefined}>
      <Guarded>
        <AuthProvider>
          <ModulesProvider>
            <Gate />
          </ModulesProvider>
        </AuthProvider>
      </Guarded>
    </Router>
  )
}
