import type { ReactNode } from 'react'
import { Navigate, Route, BrowserRouter as Router, Routes } from 'react-router-dom'
import { isAdmin } from './api'
import { AuthProvider, useAuth } from './AuthContext'
import AppShell from './components/AppShell'
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
import Search from './pages/Search'
import SeriesDetail from './pages/SeriesDetail'
import ShowDetail from './pages/ShowDetail'
import DownloadSettings from './pages/settings/DownloadSettings'
import SystemSettings from './pages/settings/SystemSettings'
import IndexerSettings from './pages/settings/IndexerSettings'
import MediaSettings from './pages/settings/MediaSettings'
import MetadataSettings from './pages/settings/MetadataSettings'
import MediaServerSettings from './pages/settings/MediaServerSettings'
import NotificationSettings from './pages/settings/NotificationSettings'
import QualitySettings from './pages/settings/QualitySettings'
import SettingsLayout from './pages/settings/SettingsLayout'
import SubtitleSettings from './pages/settings/SubtitleSettings'
import VPNSettings from './pages/settings/VPNSettings'

// Pages only an administrator can use. A member who opens one (for example
// by typing its address) is sent somewhere they can use instead.
function AdminOnly({ children, fallback = '/settings/profile' }: { children: ReactNode; fallback?: string }) {
  const { user } = useAuth()
  return isAdmin(user) ? <>{children}</> : <Navigate to={fallback} replace />
}

function Gate() {
  const { loading, offline, firstRunNeeded, user, needsWizard } = useAuth()
  const admin = isAdmin(user)

  if (loading) {
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
      <Route element={<AppShell />}>
        <Route index element={<Dashboard />} />
        <Route path="/search" element={<Search />} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/discover/all" element={<DiscoverAll />} />
        <Route path="/library" element={<Library />} />
        <Route path="/import" element={<AdminOnly fallback="/library"><ImportLibrary /></AdminOnly>} />
        <Route path="/title/:tmdbId" element={<MovieDetail />} />
        <Route path="/series/:id" element={<SeriesDetail />} />
        <Route path="/show/:tmdbId" element={<ShowDetail />} />
        <Route path="/wanted" element={<Upcoming tab="wanted" />} />
        <Route path="/calendar" element={<Upcoming tab="calendar" />} />
        <Route path="/queue" element={<Queue />} />
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to={admin ? 'media' : 'profile'} replace />} />
          <Route path="media" element={<AdminOnly><MediaSettings /></AdminOnly>} />
          <Route path="quality" element={<AdminOnly><QualitySettings /></AdminOnly>} />
          <Route path="indexers" element={<AdminOnly><IndexerSettings /></AdminOnly>} />
          <Route path="downloads" element={<AdminOnly><DownloadSettings /></AdminOnly>} />
          <Route path="vpn" element={<AdminOnly><VPNSettings /></AdminOnly>} />
          <Route path="subtitles" element={<AdminOnly><SubtitleSettings /></AdminOnly>} />
          <Route path="metadata" element={<AdminOnly><MetadataSettings /></AdminOnly>} />
          <Route path="media-servers" element={<AdminOnly><MediaServerSettings /></AdminOnly>} />
          <Route path="notifications" element={<AdminOnly><NotificationSettings /></AdminOnly>} />
          <Route path="profile" element={<Profile />} />
          <Route path="system" element={<AdminOnly><SystemSettings /></AdminOnly>} />
          <Route path="about" element={<About />} />
          <Route path="general" element={<Navigate to="/settings/system" replace />} />
        </Route>
        <Route path="/profile" element={<Navigate to="/settings/profile" replace />} />
        <Route path="/about" element={<Navigate to="/settings/about" replace />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <Router>
      <AuthProvider>
        <Gate />
      </AuthProvider>
    </Router>
  )
}
