package api

import (
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// "Try this profile" (Settings > Library > Quality): a release name, and
// optionally a size and an indexer, is run through a quality profile the way
// a search would run it, and the answer says how the name reads, how it
// scores and whether an automatic search would take it. It reads settings and
// profiles only: nothing is searched, grabbed or saved.

// maxSimulatedTitle keeps a pasted name to a sane length.
const maxSimulatedTitle = 500

type simulateRequest struct {
	Title     string  `json:"title"`
	ProfileID int64   `json:"profileId"`
	Kind      string  `json:"kind"`     // "movie" (default) or "tv": which default profile, size table and runtime rules apply
	SizeMB    float64 `json:"sizeMB"`   // 0 = the size is not known
	Indexer   string  `json:"indexer"`  // an indexer's name, for its seeder minimum and priority
	Protocol  string  `json:"protocol"` // "usenet" or "torrent"; with an indexer, the indexer decides
	Seeders   *int    `json:"seeders"`  // torrents only
}

type simulateResponse struct {
	Parsed       parsedView          `json:"parsed"`
	Profile      string              `json:"profile"`
	Verdict      string              `json:"verdict"` // "grab" or "reject"
	Summary      string              `json:"summary"`
	Score        *int                `json:"score,omitempty"`
	Formats      []quality.FormatHit `json:"formats,omitempty"`
	Terms        []preferredHit      `json:"terms,omitempty"`
	FixScore     int                 `json:"fixScore,omitempty"`
	MinScore     int                 `json:"minScore,omitempty"`
	UpgradeUntil int                 `json:"upgradeUntil,omitempty"`
	Checks       []releaseCheck      `json:"checks"`
	Reasons      []releaseReason     `json:"reasons"`
	AcceptedBy   *acceptedByPayload  `json:"acceptedBy,omitempty"`
}

// simulateRelease runs one release name through a profile. The pieces are the
// ones the search list uses, so the answer is the one a real search would
// give for a release with that name.
func (s *Server) simulateRelease(req simulateRequest) (simulateResponse, error) {
	profiles, err := s.loadProfiles()
	if err != nil {
		return simulateResponse{}, err
	}
	kind := "movie"
	profile := profiles.resolve(req.ProfileID)
	if req.Kind == "tv" {
		kind = "tv"
		profile = profiles.resolveTV(req.ProfileID)
	}
	res := indexers.Result{Title: strings.TrimSpace(req.Title), Protocol: indexers.ProtocolUsenet, SizeBytes: int64(req.SizeMB * 1000 * 1000)}
	if req.Protocol == string(indexers.ProtocolTorrent) {
		res.Protocol = indexers.ProtocolTorrent
	}
	if inst, ok := s.indexerNamed(req.Indexer); ok {
		res.IndexerName, res.Priority, res.MinSeeders = inst.Name, inst.Priority, inst.MinSeeders
		if req.Protocol == "" {
			res.Protocol = inst.Protocol
		}
	}
	if res.Protocol == indexers.ProtocolTorrent && req.Seeders != nil {
		res.Seeders, res.SeedersKnown = max(*req.Seeders, 0), true
	}

	rel := res.Parsed()
	p := searchResultPayload{
		Title: res.Title, SizeBytes: res.SizeBytes, Protocol: string(res.Protocol), Seeders: res.Seeders,
		Resolution: rel.Resolution, Source: rel.Source, Codec: rel.Codec, Group: rel.Group,
	}
	if belowMinSeeders(res) {
		p.BelowMinSeeders, p.MinSeeders = true, res.MinSeeders
	}
	p.setLanguage(quality.FitLanguageOnly(rel, profile.Language, profile.LanguageOnly))
	p.addReasonList(rejectionReasonsFor(res.Title, profile, false, "", "", keepsNothing))
	if res.SizeBytes > 0 {
		p.addReasons("size", s.sizeRejections(s.sizeLimitsFor(kind), 0, profile, res.Title, res.SizeBytes, simulatedEpisodes(rel)))
	}
	p.addStateReasons()
	setFormatScore(&p, profile, res.Title)
	p.setPreferredHits(profile, res.Title)
	p.AcceptedBy = acceptedBy(profile, res.Title)
	checks := explainChecks(p, res, profile)

	out := simulateResponse{
		Parsed: parsedViewOf(rel), Profile: profile.Name, Score: p.Score, Formats: p.Formats, Terms: p.Terms, FixScore: p.FixScore,
		MinScore: p.MinScore, UpgradeUntil: p.UpgradeUntil, Checks: checks, Reasons: p.Reasons, AcceptedBy: p.AcceptedBy,
	}
	if out.Reasons == nil {
		out.Reasons = []releaseReason{}
	}
	if bestCandidate(p, res, profile) {
		out.Verdict, out.Summary = "grab", "An automatic search would take this release."
	} else {
		out.Verdict, out.Summary = "reject", "An automatic search would not take this release."
	}
	return out, nil
}

// simulatedEpisodes is how many episodes the size table counts a name for: a
// season pack counts as a pack, a single episode as one, a movie as none.
func simulatedEpisodes(rel parser.Release) int {
	if rel.Season == 0 && rel.AirDate == "" && len(rel.Absolute) == 0 {
		return 0
	}
	if n := len(rel.Episodes); n > 0 {
		return n
	}
	if rel.AirDate != "" || len(rel.Absolute) > 0 {
		return 1
	}
	return quality.SeasonPack
}

// indexerNamed finds an indexer by its name, ignoring case.
func (s *Server) indexerNamed(name string) (indexers.Instance, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return indexers.Instance{}, false
	}
	all, err := s.IndexerRepo.List()
	if err != nil {
		return indexers.Instance{}, false
	}
	for _, inst := range all {
		if strings.EqualFold(inst.Name, name) {
			return inst, true
		}
	}
	return indexers.Instance{}, false
}

// handleSimulateProfile answers "Try this profile".
func (s *Server) handleSimulateProfile(w http.ResponseWriter, r *http.Request) {
	var req simulateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid request.")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > maxSimulatedTitle {
		writeError(w, http.StatusBadRequest, "Paste a release name to try.")
		return
	}
	out, err := s.simulateRelease(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
