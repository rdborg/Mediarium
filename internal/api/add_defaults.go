package api

import "github.com/rdborg/mediarium/internal/settings"

// The two choices the add dialog starts with. Both are on unless switched off
// in Settings, which is how every install behaved before the setting existed.
// They only set the starting state: each title can still be changed when it is
// added. Requests, import lists and the API's own defaults are not affected.

// addMonitoredDefault is whether a new title starts out monitored in the add dialog.
func (s *Server) addMonitoredDefault() bool { return s.settingOn(settings.KeyAddMonitored, true) }

// addSearchNowDefault is whether "start searching now" starts out ticked in the add dialog.
func (s *Server) addSearchNowDefault() bool { return s.settingOn(settings.KeyAddSearchNow, true) }

// addKeepBetterDefault is whether a title added without saying otherwise keeps
// being searched for better versions once it is downloaded (the add dialog's
// "Keep looking for better versions" box, and the answer for an import list or a
// script that does not say). It is off unless switched on, which is how every
// install behaved before the setting existed; the setup wizard switches it on for
// a new install. A title's own choice, and its quality profile's switch, still
// decide what happens to it.
func (s *Server) addKeepBetterDefault() bool { return s.settingOn(settings.KeyAddKeepBetter, false) }

// noUpgradeByDefault is the "leave what I have alone" a new title gets when the
// request does not say.
func (s *Server) noUpgradeByDefault() bool { return !s.addKeepBetterDefault() }
