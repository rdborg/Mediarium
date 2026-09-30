package api

import (
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// preferredLanguage is the audio language wanted (Settings > Library >
// Quality). English until it is changed.
func (s *Server) preferredLanguage() string {
	if v, _ := s.Settings.Get(settings.KeyQualityLanguage); v != "" {
		return v
	}
	return quality.DefaultLanguage
}
