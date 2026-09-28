import { Navigate, Route, BrowserRouter as Router, Routes } from 'react-router-dom'
import { AuthProvider, useAuth } from './AuthContext'
import AppShell from './components/AppShell'
import About from './pages/About'
import Calendar from './pages/Calendar'
import Dashboard from './pages/Dashboard'
import Discover from './pages/Discover'
import Library from './pages/Library'
import Login from './pages/Login'
import MovieDetail from './pages/MovieDetail'
import Onboarding from './pages/Onboarding'
import Profile from './pages/Profile'
import Queue from './pages/Queue'
import ImportLibrary from './pages/ImportLibrary'
import Search from './pages/Search'
import Wanted from './pages/Wanted'
import SeriesDetail from './pages/SeriesDetail'
import ShowDetail from './pages/ShowDetail'
import DownloadSettings from './pages/settings/DownloadSettings'
import SystemSettings from './pages/settings/SystemSettings'
import IndexerSettings from './pages/settings/IndexerSettings'
import MediaSettings from './pages/settings/MediaSettings'
import MetadataSettings from './pages/settings/MetadataSettings'
import NotificationSettings from './pages/settings/NotificationSettings'
import QualitySettings from './pages/settings/QualitySettings'
import SettingsLayout from './pages/settings/SettingsLayout'
import SubtitleSettings from './pages/settings/SubtitleSettings'
import VPNSettings from './pages/settings/VPNSettings'

function Gate() {
  const { loading, firstRunNeeded, user } = useAuth()

  if (loading) {
    return <div className="empty-state">Loading…</div>
  }
  if (firstRunNeeded) {
    return <Onboarding />
  }
  if (!user) {
    return <Login />
  }

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Dashboard />} />
        <Route path="/search" element={<Search />} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/library" element={<Library />} />
        <Route path="/import" element={<ImportLibrary />} />
        <Route path="/title/:tmdbId" element={<MovieDetail />} />
        <Route path="/series/:id" element={<SeriesDetail />} />
        <Route path="/show/:tmdbId" element={<ShowDetail />} />
        <Route path="/wanted" element={<Wanted />} />
        <Route path="/calendar" element={<Calendar />} />
        <Route path="/queue" element={<Queue />} />
        <Route path="/settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to="media" replace />} />
          <Route path="media" element={<MediaSettings />} />
          <Route path="quality" element={<QualitySettings />} />
          <Route path="indexers" element={<IndexerSettings />} />
          <Route path="downloads" element={<DownloadSettings />} />
          <Route path="vpn" element={<VPNSettings />} />
          <Route path="subtitles" element={<SubtitleSettings />} />
          <Route path="metadata" element={<MetadataSettings />} />
          <Route path="notifications" element={<NotificationSettings />} />
          <Route path="profile" element={<Profile />} />
          <Route path="system" element={<SystemSettings />} />
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
