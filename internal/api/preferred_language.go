package api

import (
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// preferredLanguage is the audio language wanted (Settings > Library and files >
// Quality). English until it is changed.
func (s *Server) preferredLanguage() string {
	if v, _ := s.Settings.Get(settings.KeyQualityLanguage); v != "" {
		return v
	}
	return quality.DefaultLanguage
}

// languageOnly is the switch "Only take releases in these languages" (off
// until it is turned on): a release has to say it is in a wanted language.
func (s *Server) languageOnly() bool {
	return s.settingOn(settings.KeyQualityLanguageOnly, false)
}

// languageIgnoreExtra is the switch "Extra languages don't change the
// ranking" (off until it is turned on).
func (s *Server) languageIgnoreExtra() bool {
	return s.settingOn(settings.KeyQualityLanguageIgnoreExtra, false)
}

// infoLanguage is the language titles, plots, genres and posters are read in
// (Settings > Library and files > Movie and show info). English until
// it is changed.
func (s *Server) infoLanguage() string {
	if v, _ := s.Settings.Get(settings.KeyInfoLanguage); metadata.IsInfoLanguage(v) {
		return v
	}
	return metadata.DefaultLanguage
}

// displayTMDB is the TMDB client for what is read on screen: Discover, search
// results and title pages, in the chosen language. New movie and show folders
// and file names use that title too (folder_title.go). The titles searched
// for on indexers use TMDB() instead, which always asks in English, so
// choosing another language never re-searches anything.
func (s *Server) displayTMDB() *metadata.Client {
	return s.TMDB().Localized(s.infoLanguage())
}
