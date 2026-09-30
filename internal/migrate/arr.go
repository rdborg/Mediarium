package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The shapes below are the parts of the Radarr/Sonarr v3 and Prowlarr v1
// APIs the importer reads. Unknown fields are ignored, so newer versions
// that add fields keep working.

type arrStatus struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

type arrQuality struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Resolution int    `json:"resolution"`
}

type arrProfileItem struct {
	ID      int              `json:"id"` // set on groups (1000+)
	Name    string           `json:"name"`
	Quality *arrQuality      `json:"quality"`
	Items   []arrProfileItem `json:"items"`
	Allowed bool             `json:"allowed"`
}

type arrProfile struct {
	ID             int              `json:"id"`
	Name           string           `json:"name"`
	UpgradeAllowed bool             `json:"upgradeAllowed"`
	Cutoff         int              `json:"cutoff"`
	Items          []arrProfileItem `json:"items"`
}

type arrRootFolder struct {
	Path string `json:"path"`
}

type arrMovieFile struct {
	RelativePath string `json:"relativePath"`
	Path         string `json:"path"`
	Quality      struct {
		Quality arrQuality `json:"quality"`
	} `json:"quality"`
}

type arrMovie struct {
	Title            string        `json:"title"`
	Year             int           `json:"year"`
	TMDBID           int           `json:"tmdbId"`
	Monitored        bool          `json:"monitored"`
	HasFile          bool          `json:"hasFile"`
	Path             string        `json:"path"`
	RootFolderPath   string        `json:"rootFolderPath"`
	QualityProfileID int           `json:"qualityProfileId"`
	MovieFile        *arrMovieFile `json:"movieFile"`
}

type arrSeason struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
}

type arrSeriesStats struct {
	EpisodeFileCount int   `json:"episodeFileCount"`
	EpisodeCount     int   `json:"episodeCount"`
	SizeOnDisk       int64 `json:"sizeOnDisk"`
}

type arrSeries struct {
	Title            string         `json:"title"`
	Year             int            `json:"year"`
	TVDBID           int            `json:"tvdbId"`
	TMDBID           int            `json:"tmdbId"` // Sonarr v4 and later
	Monitored        bool           `json:"monitored"`
	Path             string         `json:"path"`
	RootFolderPath   string         `json:"rootFolderPath"`
	QualityProfileID int            `json:"qualityProfileId"`
	Seasons          []arrSeason    `json:"seasons"`
	Statistics       arrSeriesStats `json:"statistics"`
}

type prowlarrField struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type prowlarrCategory struct {
	ID            int                `json:"id"`
	SubCategories []prowlarrCategory `json:"subCategories"`
}

type prowlarrIndexer struct {
	Name           string          `json:"name"`
	Implementation string          `json:"implementation"`
	DefinitionName string          `json:"definitionName"`
	Protocol       string          `json:"protocol"`
	Enable         bool            `json:"enable"`
	Priority       int             `json:"priority"`
	Fields         []prowlarrField `json:"fields"`
	Capabilities   struct {
		Categories []prowlarrCategory `json:"categories"`
	} `json:"capabilities"`
}

// field returns a field's value as text ("" when missing or null). Numbers
// and booleans come back in their JSON spelling.
func (p prowlarrIndexer) field(name string) (string, bool) {
	for _, f := range p.Fields {
		if f.Name != name {
			continue
		}
		return rawText(f.Value), true
	}
	return "", false
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func (p prowlarrIndexer) categoryIDs() []int {
	var out []int
	var walk func([]prowlarrCategory)
	walk = func(cats []prowlarrCategory) {
		for _, c := range cats {
			out = append(out, c.ID)
			walk(c.SubCategories)
		}
	}
	walk(p.Capabilities.Categories)
	return out
}

// masked reports whether a secret was hidden by the app that returned it
// (newer Servarr and SABnzbd versions send "********" instead of a
// password or key).
func masked(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && strings.Trim(s, "*") == ""
}

// checkApp confirms the status answer came from the expected app, so a
// Sonarr address typed into the Radarr box is caught up front.
func checkApp(st arrStatus, want string) error {
	if st.AppName != "" && !strings.EqualFold(st.AppName, want) {
		return fmt.Errorf("this address is %s, not %s", st.AppName, want)
	}
	return nil
}

type radarrData struct {
	Version     string
	Movies      []arrMovie
	Profiles    []arrProfile
	RootFolders []arrRootFolder
}

func fetchRadarr(ctx context.Context, hc *http.Client, c Conn) (radarrData, error) {
	var (
		d  radarrData
		st arrStatus
	)
	if err := get(ctx, hc, c, "/api/v3/system/status", nil, false, &st); err != nil {
		return d, err
	}
	if err := checkApp(st, "Radarr"); err != nil {
		return d, err
	}
	d.Version = st.Version
	if err := get(ctx, hc, c, "/api/v3/movie", nil, false, &d.Movies); err != nil {
		return d, fmt.Errorf("read movies: %w", err)
	}
	if err := get(ctx, hc, c, "/api/v3/qualityprofile", nil, false, &d.Profiles); err != nil {
		return d, fmt.Errorf("read quality profiles: %w", err)
	}
	if err := get(ctx, hc, c, "/api/v3/rootfolder", nil, false, &d.RootFolders); err != nil {
		return d, fmt.Errorf("read root folders: %w", err)
	}
	return d, nil
}

type sonarrData struct {
	Version     string
	Series      []arrSeries
	Profiles    []arrProfile
	RootFolders []arrRootFolder
}

func fetchSonarr(ctx context.Context, hc *http.Client, c Conn) (sonarrData, error) {
	var (
		d  sonarrData
		st arrStatus
	)
	if err := get(ctx, hc, c, "/api/v3/system/status", nil, false, &st); err != nil {
		return d, err
	}
	if err := checkApp(st, "Sonarr"); err != nil {
		return d, err
	}
	d.Version = st.Version
	if err := get(ctx, hc, c, "/api/v3/series", nil, false, &d.Series); err != nil {
		return d, fmt.Errorf("read series: %w", err)
	}
	if err := get(ctx, hc, c, "/api/v3/qualityprofile", nil, false, &d.Profiles); err != nil {
		return d, fmt.Errorf("read quality profiles: %w", err)
	}
	if err := get(ctx, hc, c, "/api/v3/rootfolder", nil, false, &d.RootFolders); err != nil {
		return d, fmt.Errorf("read root folders: %w", err)
	}
	return d, nil
}

type prowlarrData struct {
	Version  string
	Indexers []prowlarrIndexer
}

func fetchProwlarr(ctx context.Context, hc *http.Client, c Conn) (prowlarrData, error) {
	var (
		d  prowlarrData
		st arrStatus
	)
	if err := get(ctx, hc, c, "/api/v1/system/status", nil, false, &st); err != nil {
		return d, err
	}
	if err := checkApp(st, "Prowlarr"); err != nil {
		return d, err
	}
	d.Version = st.Version
	if err := get(ctx, hc, c, "/api/v1/indexer", nil, false, &d.Indexers); err != nil {
		return d, fmt.Errorf("read indexers: %w", err)
	}
	return d, nil
}

// flexInt reads a number SABnzbd may send as 1, "1", true or "".
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	switch strings.ToLower(s) {
	case "", "null", "false":
		*f = 0
		return nil
	case "true":
		*f = 1
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("not a number: %q", s)
	}
	*f = flexInt(n)
	return nil
}

type sabServer struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"displayname"`
	Host        string  `json:"host"`
	Port        flexInt `json:"port"`
	Username    string  `json:"username"`
	Password    string  `json:"password"`
	Connections flexInt `json:"connections"`
	SSL         flexInt `json:"ssl"`
	Enable      flexInt `json:"enable"`
	Optional    flexInt `json:"optional"`
	Priority    flexInt `json:"priority"`
}

type sabCategory struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type sabConfig struct {
	Config struct {
		Misc struct {
			CompleteDir string `json:"complete_dir"`
			DownloadDir string `json:"download_dir"`
		} `json:"misc"`
		Servers    []sabServer   `json:"servers"`
		Categories []sabCategory `json:"categories"`
	} `json:"config"`
	// A wrong key answers {"status": false, "error": "API Key Incorrect"}.
	Status *bool  `json:"status"`
	Error  string `json:"error"`
}

type sabData struct {
	Version string
	Config  sabConfig
}

func fetchSABnzbd(ctx context.Context, hc *http.Client, c Conn) (sabData, error) {
	var d sabData
	var ver struct {
		Version string `json:"version"`
	}
	if err := get(ctx, hc, c, "/api", url.Values{"mode": {"version"}, "output": {"json"}}, true, &ver); err != nil {
		return d, err
	}
	d.Version = ver.Version
	if err := get(ctx, hc, c, "/api", url.Values{"mode": {"get_config"}, "output": {"json"}}, true, &d.Config); err != nil {
		return d, fmt.Errorf("read settings: %w", err)
	}
	if d.Config.Error != "" || (d.Config.Status != nil && !*d.Config.Status) {
		msg := d.Config.Error
		if strings.Contains(strings.ToLower(msg), "key") {
			return d, fmt.Errorf("the API key was not accepted (use the API Key from Config > General, not the NZB Key)")
		}
		return d, fmt.Errorf("SABnzbd refused the request: %s", msg)
	}
	return d, nil
}
