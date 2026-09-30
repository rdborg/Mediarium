package migrate

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Medusa and SickChill are older alternatives to Sonarr. Their shows are
// imported like Sonarr's: by TVDB id (mapped to TMDB), with the paused flag
// as monitored, the show folder through the same path map, and the episode
// files in it registered where they are. They do not say how many files a
// show has (or the listing that would is not read), so a show whose folder is
// not found is skipped rather than risk downloading it again.

// legacyShow is one show of Medusa or SickChill.
type legacyShow struct {
	Title    string
	Year     int
	TVDBID   int
	TMDBID   int
	Location string // the show's folder as the app sees it
	Paused   bool
}

// planLegacyShows adds the shows of app (Medusa or SickChill) to the plan.
func (im *Importer) planLegacyShows(ctx context.Context, p *plan, tp *TitlesPreview, app string, shows []legacyShow, st state, maps []PathMapping) {
	tmdbIDs := make([]int, len(shows))
	lookupErrs := make([]error, len(shows))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, lookupWorkers)
	)
	for i, s := range shows {
		if s.TMDBID > 0 || s.TVDBID <= 0 {
			tmdbIDs[i] = s.TMDBID
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tmdbIDs[i], lookupErrs[i] = im.tmdbForTVDB(ctx, s.TVDBID)
		}()
	}
	wg.Wait()

	for i, s := range shows {
		folder, _ := translate(s.Location, maps)
		item := TitleItem{
			Title: s.Title, Year: s.Year, TMDBID: tmdbIDs[i], TVDBID: s.TVDBID, Monitored: !s.Paused, ArrPath: s.Location, Path: folder,
			FolderFound: isDir(folder), InLibraryFolder: insideFolder(folder, st.tvRoot),
		}
		_, exists := st.series[item.TMDBID]
		switch {
		case lookupErrs[i] != nil:
			item.Action, item.Reason = ActionSkip, "TMDB lookup failed: "+lookupErrs[i].Error()
		case item.TMDBID == 0 && s.TVDBID > 0:
			item.Action, item.Reason = ActionSkip, fmt.Sprintf("no TMDB id: TMDB has no show with TVDB id %d", s.TVDBID)
		case item.TMDBID == 0:
			item.Action, item.Reason = ActionSkip, "no TMDB id"
		case exists && item.FolderFound:
			item.Action, item.Reason = ActionExists, "already in your library; episodes in its folder not linked yet will be"
		case exists:
			item.Action, item.Reason = ActionExists, "already in your library"
		case s.Location == "":
			item.Action, item.Reason = ActionAdd, app+" gave no folder for it: added without linking files, no search is started"
		case !item.FolderFound:
			item.Action, item.Reason = ActionSkip, "folder not found at "+folder
		default:
			item.Action, item.Reason = ActionAdd, "the episode files in its folder are linked where they are"
		}
		if item.Action == ActionAdd && s.Paused {
			item.Reason += "; paused in " + app + ", so added unmonitored"
		}
		libraryNote(&item, st.tvRoot, "TV")
		tp.Summary.count(item.Action)
		tp.Items = append(tp.Items, item)
		// -1: the episode file count is unknown, the folder is scanned.
		var stats arrSeriesStats
		stats.EpisodeFileCount = -1
		p.series = append(p.series, seriesPlan{
			item:   item,
			series: arrSeries{Title: s.Title, Year: s.Year, TVDBID: s.TVDBID, TMDBID: item.TMDBID, Monitored: !s.Paused, Path: s.Location, Statistics: stats},
			folder: folder,
			app:    app,
		})
	}
}

// legacyPaths are the show folders of the legacy apps that were read, by
// app, so their root folders take part in the folder mapping suggestions.
func (e *extraData) legacyPaths() map[string][]string {
	out := map[string][]string{}
	collect := func(name string, shows []legacyShow, err error, present bool) {
		if !present || err != nil {
			return
		}
		paths := []string{}
		for _, s := range shows {
			if s.Location != "" {
				paths = append(paths, s.Location)
			}
		}
		out[name] = paths
	}
	collect("Medusa", e.medusa, e.medusaErr, e.medusaOn)
	collect("SickChill", e.sickchill, e.sickchillErr, e.sickchillOn)
	return out
}

func newTitlesPreview(version string, err error) *TitlesPreview {
	return &TitlesPreview{AppStatus: appStatus(version, err), RootFolders: []RootFolder{}, Profiles: []ProfileMapping{}, Items: []TitleItem{}}
}

// yearFromTitle reads a trailing "(2008)" some apps add to a show's name.
func yearFromTitle(title string) (string, int) {
	t := strings.TrimSpace(title)
	if len(t) > 7 && strings.HasSuffix(t, ")") && t[len(t)-6] == '(' {
		if y := yearOf(t[len(t)-5 : len(t)-1]); y > 0 {
			return strings.TrimSpace(t[:len(t)-6]), y
		}
	}
	return t, 0
}
