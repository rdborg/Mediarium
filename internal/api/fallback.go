package api

import (
	"fmt"
	"log/slog"

	"github.com/ryanborg/mediarium/internal/quality"
)

// Quality fallback chain: a profile may name other profiles to try, in
// order, when an automatic search finds nothing the profile itself accepts
// (quality.Profile.Fallback). The item keeps its own profile, so the normal
// upgrade hunt replaces a fallback download as soon as a release the item's
// own profile accepts turns up (quality.Profile.OnFallback).

// acceptedByPayload names the profile that would accept a search result.
type acceptedByPayload struct {
	ProfileID   int64  `json:"profileId"`
	ProfileName string `json:"profileName"`
	// Fallback is true when the item's own profile does not accept the
	// release and one of its fallbacks does: automation only takes it when
	// nothing the own profile accepts is found.
	Fallback bool `json:"fallback"`
}

// acceptedBy reports which profile of the item's chain accepts a release
// title, or nil when none does.
func acceptedBy(profile quality.Profile, title string) *acceptedByPayload {
	by, fallback, ok := profile.AcceptedBy(title)
	if !ok {
		return nil
	}
	return &acceptedByPayload{ProfileID: by.ID, ProfileName: by.Name, Fallback: fallback}
}

// noteFallbackGrab records, when an automatic grab was only acceptable to a
// fallback profile, which profile was used: in the log and in the item's
// activity. movieID is 0 for a TV grab, seriesID 0 for a movie.
func (s *Server) noteFallbackGrab(movieID, seriesID int64, itemTitle string, profile quality.Profile, releaseTitle string) {
	by, fallback, ok := profile.AcceptedBy(releaseTitle)
	if !ok || !fallback {
		return
	}
	slog.Info("automation: grabbed with a fallback profile",
		"item", itemTitle, "movieId", movieID, "seriesId", seriesID, "release", releaseTitle,
		"profile", profile.Name, "fallbackProfile", by.Name)
	message := fmt.Sprintf("%s: nothing acceptable to the %q profile, so %q was grabbed with the fallback profile %q; it will be replaced when a %q release appears",
		itemTitle, profile.Name, releaseTitle, by.Name, profile.Name)
	if err := s.QueueRepo.LogItemActivity(movieID, seriesID, "grabbed", message); err != nil {
		slog.Warn("automation: record fallback grab", "item", itemTitle, "err", err)
	}
}
