import { lazy, Suspense, type ReactNode } from 'react'
import { Navigate, Route, BrowserRouter as Router, Routes, useLocation } from 'react-router-dom'
import { isAdmin } from './api'
import { AuthProvider, useAuth } from './AuthContext'
import { ModulesProvider } from './ModulesContext'
import AppShell from './components/AppShell'
import ErrorBoundary from './components/ErrorBoundary'
import About from './pages/About'
import Upcoming from './pages/Upcoming'
import Dashboard from './pages/Dashboard'
import Discover from './pages/Discover'
import DiscoverAll from './pages/DiscoverAll'
import Splash from './components/Splash'
import Library from './pages/Library'
import Login from './pages/Login'
import MovieDetail from './pages/MovieDetail'
import Onboarding from './pages/Onboarding'
import Profile from './pages/Profile'
import Queue from './pages/Queue'
import ImportLibrary from './pages/ImportLibrary'
import MusicImport from './pages/MusicImport'
import MusicDiscoverAll from './pages/MusicDiscoverAll'
import MusicArtist from './pages/MusicArtist'
import BookDetail from './pages/BookDetail'
import BookWork from './pages/BookWork'
import MusicAlbumRedirect from './pages/MusicAlbumRedirect'
import Search from './pages/Search'
import SeriesDetail from './pages/SeriesDetail'
import ShowDetail from './pages/ShowDetail'
import DownloadSettings from './pages/settings/DownloadSettings'
import SystemSettings from './pages/settings/SystemSettings'
import LogsSettings from './pages/settings/LogsSettings'
import IndexerSettings from './pages/settings/IndexerSettings'
import MediaSettings from './pages/settings/MediaSettings'
import ModulesSettings from './pages/settings/ModulesSettings'
import MetadataSettings from './pages/settings/MetadataSettings'
import MediaServerSettings from './pages/settings/MediaServerSettings'
import NotFound from './pages/NotFound'
import Statistics from './pages/Statistics'
import ManualImport from './pages/ManualImport'
import ReleaseSearch from './pages/ReleaseSearch'
import MigrateSettings from './pages/settings/MigrateSettings'
import NotificationSettings from './pages/settings/NotificationSettings'
import QualitySettings from './pages/settings/QualitySettings'
import SettingsLayout from './pages/settings/SettingsLayout'
import SubtitleSettings from './pages/settings/SubtitleSettings'
import VPNSettings from './pages/settings/VPNSettings'

// Mediarium Books (reader and player), an app of its own, loaded when opened.
const BookshelfApp = lazy(() => import('./bookshelf/BookshelfApp'))

// Pages only an administrator can use. A member who opens one (for example
// by typing its address) is sent somewhere they can use instead.
function AdminOnly({ children, fallback = '/settings/profile' }: { children: ReactNode; fallback?: string }) {
  const { user } = useAuth()
  return isAdmin(user) ? <>{children}</> : <Navigate to={fallback} replace />
}

function Gate() {
  const { loading, offline, slow, firstRunNeeded, user, needsWizard, refresh } = useAuth()
  const admin = isAdmin(user)

  if (loading) {
    if (slow) {
      return (
        <Splash
          label="Mediarium is slow to answer"
          hint="It may be busy with a download or an import. It keeps trying by itself."
          onRetry={() => void refresh()}
        />
      )
    }
    return <Splash label={offline ? "Reconnecting to Mediarium" : "Loading"} />
  }
  if (firstRunNeeded) {
    return <Onboarding />
  }
  if (!user) {
    return <Login />
  }
  if (needsWizard) {
    return <Onboarding startStep={1} />
  }

  return (
    <Routes>
      <Route
        path="/bookshelf/*"
        element={
          <Suspense fallback={<Splash label="Loading" />}>
            <BookshelfApp />
          </Suspense>
        }
      />
      <Route element={<AppShell />}>
        <Route index element={<Dashboard />} />
        <Route path="/search" element={<Search />} />
        <Route path="/search/releases" element={<ReleaseSearch />} />
        <Route path="/stats" element={<Statistics />} />
        <Route path="/import/manual" element={<AdminOnly><ManualImport /></AdminOnly>} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/discover/all" element={<DiscoverAll />} />
        <Route path="/discover/music" element={<MusicDiscoverAll />} />
        <Route path="/library" element={<Library />} />
        <Route path="/import" element={<AdminOnly fallback="/library"><ImportLibrary /></AdminOnly>} />
        <Route path="/music/artist/:id" element={<MusicArtist />} />
        <Route path="/music/album/:id" element={<MusicAlbumRedirect />} />
        <Route path="/book/:id" element={<BookDetail />} />
        <Route path="/books/work/:key" element={<BookWork />} />
        <Route path="/music/import" element={<AdminOnly fallback="/library"><MusicImport /></AdminOnly>} />
        <Route path="/title/:tmdbId" element={<MovieDetail />} />
        <Route path="/series/:id" element={<SeriesDetail />} />
        <Route path="/show/:tmdbId" element={<ShowDetail />} />
        <Route path="/wanted" element={<Upcoming tab="wanted" />} />
        <Route path="/calendar" element={<Upcoming tab="calendar" />} />
        <Route path="/queue" element={<Queue />} />
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to={admin ? 'modules' : 'profile'} replace />} />
          <Route path="modules" element={<AdminOnly><ModulesSettings /></AdminOnly>} />
          <Route path="media" element={<AdminOnly><MediaSettings /></AdminOnly>} />
          <Route path="quality" element={<AdminOnly><QualitySettings /></AdminOnly>} />
          <Route path="indexers" element={<AdminOnly><IndexerSettings /></AdminOnly>} />
          <Route path="downloads" element={<AdminOnly><DownloadSettings /></AdminOnly>} />
          <Route path="vpn" element={<AdminOnly><VPNSettings /></AdminOnly>} />
          <Route path="subtitles" element={<AdminOnly><SubtitleSettings /></AdminOnly>} />
          <Route path="metadata" element={<AdminOnly><MetadataSettings /></AdminOnly>} />
          <Route path="media-servers" element={<AdminOnly><MediaServerSettings /></AdminOnly>} />
          <Route path="migrate" element={<AdminOnly><MigrateSettings /></AdminOnly>} />
          <Route path="notifications" element={<AdminOnly><NotificationSettings /></AdminOnly>} />
          <Route path="profile" element={<Profile />} />
          <Route path="system" element={<AdminOnly><SystemSettings /></AdminOnly>} />
          <Route path="logs" element={<AdminOnly><LogsSettings /></AdminOnly>} />
          <Route path="about" element={<About />} />
          <Route path="general" element={<Navigate to="/settings/system" replace />} />
        </Route>
        <Route path="/profile" element={<Navigate to="/settings/profile" replace />} />
        <Route path="/about" element={<Navigate to="/settings/about" replace />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}

// A page that crashes while it draws shows a short message instead of a blank
// screen, and going to another page clears it.
function Guarded({ children }: { children: ReactNode }) {
  return <ErrorBoundary resetKey={useLocation().pathname}>{children}</ErrorBoundary>
}

export default function App() {
  return (
    <Router>
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
