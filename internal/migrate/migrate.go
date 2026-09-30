package migrate

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/quality"
)

// Deps is what the importer reads and writes in Mediarium.
type Deps struct {
	Library  *library.Repo
	Indexers *indexers.Repo
	Servers  *download.Repo
	Profiles *quality.Repo

	// TMDB returns the current metadata client (the key can change while
	// the app runs).
	TMDB func() *metadata.Client
	// MoviesRoot and TVRoot are Mediarium's library folders.
	MoviesRoot func() string
	TVRoot     func() string
	// DefaultProfileID is the default quality profile's id (0 = none set).
	DefaultProfileID func() int64

	// Definition finds a site definition by id (Prowlarr's definitionName
	// is the same id); it returns indexers.ErrDefinitionNotFound for an
	// unknown one. nil: sites from definitions are listed as "add by hand".
	Definition func(ctx context.Context, id string) (indexers.DefinitionSummary, error)
	// TestIndexer tests an indexer before it is saved. nil tests
	// Newznab/Torznab indexers directly and leaves site-based ones untested.
	TestIndexer func(ctx context.Context, inst indexers.Instance) error
	// Activity writes a line to the activity feed and the title's own
	// events; nil writes nothing.
	Activity func(movieID, seriesID int64, message string)

	// SubtitleLanguages and SetSubtitleLanguages read and replace the
	// subtitle languages setting (OpenSubtitles codes); nil leaves Bazarr's
	// languages out.
	SubtitleLanguages    func() []string
	SetSubtitleLanguages func(codes []string) error

	// HTTPClient talks to the other apps; nil uses one with a timeout that
	// refuses redirects to another host.
	HTTPClient *http.Client
}

// Importer reads the other apps and runs at most one import at a time.
type Importer struct {
	deps Deps
	hc   *http.Client

	mu     sync.Mutex
	status Status

	namesMu sync.Mutex
	tvdbMu  sync.Mutex
	tvdb    map[int]int          // TVDB id -> TMDB id (0 = TMDB has none)
	names   map[string]titleName // "movie:<tmdb id>" or "tv:<tmdb id>" -> title from TMDB
}

// New returns an importer writing through deps.
func New(deps Deps) *Importer {
	hc := deps.HTTPClient
	if hc == nil {
		hc = newHTTPClient()
	}
	return &Importer{deps: deps, hc: hc, tvdb: map[int]int{}, names: map[string]titleName{}, status: emptyStatus()}
}

// ErrRunning is returned by Start while an import is still going.
var ErrRunning = errors.New("an import is already running")

// Actions for a previewed item.
const (
	ActionAdd    = "add"
	ActionExists = "exists"
	ActionSkip   = "skip"
)

// Include picks what an import brings over. A field left out (null) counts
// as true.
type Include struct {
	Movies          *bool `json:"movies,omitempty"`          // Radarr
	Series          *bool `json:"series,omitempty"`          // Sonarr, Medusa, SickChill
	Indexers        *bool `json:"indexers,omitempty"`        // Prowlarr, Jackett, NZBHydra2
	UsenetServers   *bool `json:"usenetServers,omitempty"`   // SABnzbd, NZBGet
	QualityProfiles *bool `json:"qualityProfiles,omitempty"` // Radarr, Sonarr
	Requests        *bool `json:"requests,omitempty"`        // Overseerr/Jellyseerr, Ombi
	// SubtitleLanguages replaces Mediarium's subtitle languages with
	// Bazarr's. Unlike the others it is off unless set to true, since it
	// overwrites a setting.
	SubtitleLanguages *bool `json:"subtitleLanguages,omitempty"`
}

func on(b *bool) bool { return b == nil || *b }

func optIn(b *bool) bool { return b != nil && *b }

// Options are a preview or import request: the apps to read, how their
// folders map to Mediarium's, and (for an import) what to bring over.
type Options struct {
	Sources
	PathMap []PathMapping `json:"pathMap"`
	Include Include       `json:"include"`
	// ProfileMapping picks a Mediarium profile id by Radarr/Sonarr profile
	// name, overriding the automatic choice (0 = the default profile).
	ProfileMapping map[string]int64 `json:"profileMapping"`
}

// Summary counts a preview's items by action.
type Summary struct {
	Total  int `json:"total"`
	Add    int `json:"add"`
	Exists int `json:"exists"`
	Skip   int `json:"skip"`
}

func (s *Summary) count(action string) {
	s.Total++
	switch action {
	case ActionAdd:
		s.Add++
	case ActionExists:
		s.Exists++
	default:
		s.Skip++
	}
}

// AppStatus is whether an app could be read.
type AppStatus struct {
	OK      bool    `json:"ok"`
	Error   string  `json:"error,omitempty"`
	Version string  `json:"version,omitempty"`
	Summary Summary `json:"summary"`
}

// TitleItem is one movie (Radarr) or show (Sonarr) and what an import
// would do with it.
type TitleItem struct {
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	TMDBID    int    `json:"tmdbId,omitempty"`
	TVDBID    int    `json:"tvdbId,omitempty"`
	Monitored bool   `json:"monitored"`
	// ArrPath is the title's folder as Radarr/Sonarr see it, Path the same
	// folder as Mediarium sees it after the path map.
	ArrPath         string `json:"arrPath"`
	Path            string `json:"path"`
	FolderFound     bool   `json:"folderFound"`
	InLibraryFolder bool   `json:"inLibraryFolder"` // Path is inside Mediarium's movies/TV folder
	// Files is how many files Radarr/Sonarr have for it (a movie 0 or 1,
	// a show its episode files).
	Files int `json:"files"`
	// Quality is Radarr's quality of the movie's file.
	Quality string `json:"quality,omitempty"`
	// UnmonitoredSeasons lists a monitored show's seasons Sonarr doesn't
	// monitor (they stay unmonitored).
	UnmonitoredSeasons []int  `json:"unmonitoredSeasons,omitempty"`
	ArrProfile         string `json:"arrProfile,omitempty"`
	ProfileID          int64  `json:"profileId"` // Mediarium profile it gets (0 = default)
	ProfileName        string `json:"profileName,omitempty"`
	Action             string `json:"action"`
	Reason             string `json:"reason,omitempty"`
}

// TitlesPreview is Radarr's or Sonarr's part of a preview.
type TitlesPreview struct {
	AppStatus
	RootFolders []RootFolder     `json:"rootFolders"`
	Profiles    []ProfileMapping `json:"profiles"`
	Items       []TitleItem      `json:"items"`
}

// IndexerItem is one Prowlarr, Jackett or NZBHydra2 indexer.
type IndexerItem struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"` // Newznab, Torznab, Cardigann, ...
	Protocol       string `json:"protocol"`       // usenet or torrent
	BaseURL        string `json:"baseUrl,omitempty"`
	DefinitionID   string `json:"definitionId,omitempty"` // Mediarium's site definition, for sites
	Enabled        bool   `json:"enabled"`                // switched on in the other app
	Categories     []int  `json:"categories,omitempty"`
	// SourceID is the indexer's id in Jackett (what jackett.direct lists).
	SourceID string `json:"sourceId,omitempty"`
	// CanAddDirectly: Mediarium's site list has this Jackett indexer, so it
	// can be added as a site directly instead of through Jackett; Direct
	// says that was chosen.
	CanAddDirectly bool   `json:"canAddDirectly,omitempty"`
	Direct         bool   `json:"direct,omitempty"`
	Action         string `json:"action"`
	Reason         string `json:"reason,omitempty"`
}

// AggregatedIndexer is one indexer NZBHydra2 searches (for information:
// Mediarium reaches them all through NZBHydra2).
type AggregatedIndexer struct {
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
	Type  string `json:"type,omitempty"`
}

// IndexersPreview is Prowlarr's, Jackett's or NZBHydra2's part of a
// preview.
type IndexersPreview struct {
	AppStatus
	Items []IndexerItem `json:"items"`
	// Aggregated lists NZBHydra2's own indexers when its API gives them.
	Aggregated []AggregatedIndexer `json:"aggregated,omitempty"`
}

// ServerItem is one SABnzbd or NZBGet news server. The password is never
// included.
type ServerItem struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	SSL         bool   `json:"ssl"`
	Username    string `json:"username,omitempty"`
	HasPassword bool   `json:"hasPassword"` // SABnzbd gave the password (it may hide it)
	Connections int    `json:"connections"`
	Priority    int    `json:"priority"`
	Optional    bool   `json:"optional"`
	Enabled     bool   `json:"enabled"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
}

// ServersPreview is SABnzbd's or NZBGet's part of a preview.
type ServersPreview struct {
	AppStatus
	Items       []ServerItem `json:"items"`
	CompleteDir string       `json:"completeDir,omitempty"`
	Categories  []string     `json:"categories,omitempty"`
}

// Request statuses, as reported in RequestItem.Status.
const (
	RequestPending            = "pending"
	RequestApproved           = "approved"
	RequestProcessing         = "processing"
	RequestPartiallyAvailable = "partiallyAvailable"
	RequestAvailable          = "available"
	RequestDeclined           = "declined"
	RequestFailed             = "failed"
	RequestBlocked            = "blocklisted"
)

// RequestItem is one movie or show requested in Overseerr/Jellyseerr or
// Ombi (several requests for the same title are one item).
type RequestItem struct {
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	MediaType string `json:"mediaType"` // movie or tv
	TMDBID    int    `json:"tmdbId,omitempty"`
	TVDBID    int    `json:"tvdbId,omitempty"`
	Status    string `json:"status"` // pending, approved, processing, partiallyAvailable, available, declined, failed, blocklisted
	// RequestedBy are the display names of the people who asked for it.
	RequestedBy []string `json:"requestedBy"`
	// Seasons are the requested seasons of a show (monitored, with every
	// episode still to air).
	Seasons []int  `json:"seasons,omitempty"`
	Action  string `json:"action"`
	Reason  string `json:"reason,omitempty"`
}

// RequestsPreview is Overseerr's or Ombi's part of a preview.
type RequestsPreview struct {
	AppStatus
	Items []RequestItem `json:"items"`
}

// SubtitleLanguage is one language enabled in Bazarr.
type SubtitleLanguage struct {
	Name   string `json:"name"`
	Code   string `json:"code"`   // Bazarr's code
	MapsTo string `json:"mapsTo"` // Mediarium's (OpenSubtitles) code, "" when none
	Action string `json:"action"` // add (new to Mediarium), exists (already set), skip
	Reason string `json:"reason,omitempty"`
}

// SubtitleProvider is one subtitle provider enabled in Bazarr.
type SubtitleProvider struct {
	Name string `json:"name"`
	// Equivalent is the Mediarium provider doing the same job ("" when
	// Mediarium has none).
	Equivalent string `json:"equivalent,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// SubtitlesPreview is Bazarr's part of a preview. Summary counts the
// languages.
type SubtitlesPreview struct {
	AppStatus
	// Included is whether include.subtitleLanguages is on: only then does an
	// import change the languages.
	Included  bool               `json:"included"`
	Languages []SubtitleLanguage `json:"languages"`
	// Current are Mediarium's subtitle languages now, New what they become
	// when include.subtitleLanguages is true (empty: nothing usable).
	Current   []string           `json:"current"`
	New       []string           `json:"new"`
	Providers []SubtitleProvider `json:"providers"`
	// ProvidersError says why the providers couldn't be read (the
	// languages still can be).
	ProvidersError string `json:"providersError,omitempty"`
}

// Preview is what an import would do, app by app.
type Preview struct {
	Radarr    *TitlesPreview    `json:"radarr,omitempty"`
	Sonarr    *TitlesPreview    `json:"sonarr,omitempty"`
	Prowlarr  *IndexersPreview  `json:"prowlarr,omitempty"`
	SABnzbd   *ServersPreview   `json:"sabnzbd,omitempty"`
	NZBGet    *ServersPreview   `json:"nzbget,omitempty"`
	Jackett   *IndexersPreview  `json:"jackett,omitempty"`
	NZBHydra  *IndexersPreview  `json:"nzbhydra,omitempty"`
	Overseerr *RequestsPreview  `json:"overseerr,omitempty"`
	Ombi      *RequestsPreview  `json:"ombi,omitempty"`
	Bazarr    *SubtitlesPreview `json:"bazarr,omitempty"`
	Medusa    *TitlesPreview    `json:"medusa,omitempty"`
	SickChill *TitlesPreview    `json:"sickchill,omitempty"`
	// PathMap is the map in effect: the one sent plus the suggestions.
	PathMap          []PathMapping `json:"pathMap"`
	SuggestedPathMap []PathMapping `json:"suggestedPathMap"`
	MoviesPath       string        `json:"moviesPath"`
	TVPath           string        `json:"tvPath"`
}

// Preview reads the apps and reports what an import would do, changing
// nothing anywhere.
func (im *Importer) Preview(ctx context.Context, opts Options) (Preview, error) {
	snap := im.fetch(ctx, opts.Sources)
	p, err := im.plan(ctx, snap, opts)
	if err != nil {
		return Preview{}, err
	}
	return p.preview, nil
}

// titleName is a title as TMDB names it.
type titleName struct {
	Title string
	Year  int
}
