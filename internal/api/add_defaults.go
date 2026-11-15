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
