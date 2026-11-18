package migrate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/inputcheck"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// snapshot is everything read from the other apps; an app that wasn't
// asked for has a nil error and no data, one that failed has its error.
type snapshot struct {
	radarr    *radarrData
	radarrErr error
	sonarr    *sonarrData
	sonarrErr error
	prowlarr  *prowlarrData
	prowlErr  error
	sab       *sabData
	sabErr    error
	nzbget    *nzbgetData
	nzbgetErr error
	extra     extraData
}

// fetch reads every requested app at the same time.
func (im *Importer) fetch(ctx context.Context, src Sources) snapshot {
	var (
		s  snapshot
		wg sync.WaitGroup
	)
	if src.Radarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchRadarr(ctx, im.hc, *src.Radarr)
			s.radarr, s.radarrErr = &d, err
		}()
	}
	if src.Sonarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchSonarr(ctx, im.hc, *src.Sonarr)
			s.sonarr, s.sonarrErr = &d, err
		}()
	}
	if src.Prowlarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchProwlarr(ctx, im.hc, *src.Prowlarr)
			s.prowlarr, s.prowlErr = &d, err
		}()
	}
	if src.SABnzbd != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchSABnzbd(ctx, im.hc, *src.SABnzbd)
			s.sab, s.sabErr = &d, err
		}()
	}
	if src.NZBGet != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchNZBGet(ctx, im.hc, *src.NZBGet)
			s.nzbget, s.nzbgetErr = &d, err
		}()
	}
	im.fetchExtras(ctx, src, &s.extra, &wg)
	wg.Wait()
	return s
}

type moviePlan struct {
	item      TitleItem
	movie     arrMovie
	folder    string
	folderBad string // why folder can't be the movie's own: its files aren't linked
}

type seriesPlan struct {
	app       string // the app it came from, "" = Sonarr
	item      TitleItem
	series    arrSeries
	folder    string
	folderBad string // why folder can't be the show's own: its files aren't linked
}

type indexerPlan struct {
	item   IndexerItem
	inst   indexers.Instance
	secret indexers.Secrets
	masked bool // a secret was hidden by Prowlarr and couldn't be copied
}

type serverPlan struct {
	app  string
	item ServerItem
	sc   download.StoredClient
}

// plan is a preview plus what is needed to carry it out.
type plan struct {
	preview   Preview
	movies    []moviePlan
	series    []seriesPlan
	indexers  []indexerPlan
	requests  []requestPlan
	subtitles *subtitlePlan
	servers   []serverPlan
	profiles  []ProfileMapping
}

// state is Mediarium as it is before the import.
type state struct {
	movies      map[int]library.Movie
	series      map[int]library.Series
	indexers    []indexers.Instance
	servers     []download.StoredClient
	profiles    []quality.Profile
	defaultName string
	// defaultTVName is the default profile's name for shows.
	defaultTVName string
	moviesRoot    string
	tvRoot        string
	// libraryRoots are the library folders of Mediarium and of the other
	// apps (as Mediarium sees them): never one title's own folder.
	libraryRoots []string
}

func (im *Importer) loadState() (state, error) {
	st := state{movies: map[int]library.Movie{}, series: map[int]library.Series{}}
	movies, err := im.deps.Library.List()
	if err != nil {
		return st, fmt.Errorf("list movies: %w", err)
	}
	for _, m := range movies {
		st.movies[m.TMDBID] = m
	}
	shows, err := im.deps.Library.ListSeries()
	if err != nil {
		return st, fmt.Errorf("list series: %w", err)
	}
	for _, s := range shows {
		st.series[s.TMDBID] = s
	}
	if st.indexers, err = im.deps.Indexers.List(); err != nil {
		return st, fmt.Errorf("list indexers: %w", err)
	}
	if st.servers, err = im.deps.Servers.ListAll(); err != nil {
		return st, fmt.Errorf("list usenet servers: %w", err)
	}
	if st.profiles, err = im.deps.Profiles.List(); err != nil {
		return st, fmt.Errorf("list quality profiles: %w", err)
	}
	if im.deps.DefaultProfileID != nil {
		def := im.deps.DefaultProfileID()
		for _, p := range st.profiles {
			if p.ID == def {
				st.defaultName = p.Name
			}
		}
	}
	st.defaultTVName = st.defaultName
	if im.deps.DefaultTVProfileID != nil {
		if def := im.deps.DefaultTVProfileID(); def != 0 {
			for _, p := range st.profiles {
				if p.ID == def {
					st.defaultTVName = p.Name
				}
			}
		}
	}
	if im.deps.MoviesRoot != nil {
		st.moviesRoot = im.deps.MoviesRoot()
	}
	if im.deps.TVRoot != nil {
		st.tvRoot = im.deps.TVRoot()
	}
	return st, nil
}

func appStatus(version string, err error) AppStatus {
	if err != nil {
		return AppStatus{Error: err.Error()}
	}
	return AppStatus{OK: true, Version: version}
}

// plan works out, without changing anything, what an import would do.
func (im *Importer) plan(ctx context.Context, snap snapshot, opts Options) (*plan, error) {
	st, err := im.loadState()
	if err != nil {
		return nil, err
	}
	requested := cleanMappings(opts.PathMap)
	p := &plan{preview: Preview{MoviesPath: st.moviesRoot, TVPath: st.tvRoot, SuggestedPathMap: []PathMapping{}}}

	// Suggestions first, from both apps, so every title is translated with
	// the complete map.
	var radarrRoots, sonarrRoots map[string][]string
	var suggested []PathMapping
	if snap.radarr != nil && snap.radarrErr == nil {
		var paths []string
		for _, m := range snap.radarr.Movies {
			paths = append(paths, m.Path)
		}
		radarrRoots = rootsOf(snap.radarr.RootFolders, paths)
		suggested = append(suggested, suggestMappings(radarrRoots, requested, st.moviesRoot)...)
	}
	if snap.sonarr != nil && snap.sonarrErr == nil {
		var paths []string
		for _, s := range snap.sonarr.Series {
			paths = append(paths, s.Path)
		}
		sonarrRoots = rootsOf(snap.sonarr.RootFolders, paths)
		suggested = append(suggested, suggestMappings(sonarrRoots, append(append([]PathMapping{}, requested...), suggested...), st.tvRoot)...)
	}
	// The legacy apps (Medusa, SickChill) have no root folder list: their show folders give the roots.
	legacyRoots := map[string]map[string][]string{}
	legacy := snap.extra.legacyPaths()
	for _, app := range []string{"Medusa", "SickChill"} {
		paths, ok := legacy[app]
		if !ok {
			continue
		}
		roots := rootsOf(nil, paths)
		legacyRoots[app] = roots
		suggested = append(suggested, suggestMappings(roots, append(append([]PathMapping{}, requested...), suggested...), st.tvRoot)...)
	}
	effective := append(append([]PathMapping{}, requested...), suggested...)
	p.preview.PathMap = effective
	if p.preview.PathMap == nil {
		p.preview.PathMap = []PathMapping{}
	}
	p.preview.SuggestedPathMap = append(p.preview.SuggestedPathMap, suggested...)
	st.libraryRoots = []string{st.moviesRoot, st.tvRoot}
	allRoots := []map[string][]string{radarrRoots, sonarrRoots}
	for _, roots := range legacyRoots {
		allRoots = append(allRoots, roots)
	}
	for _, roots := range allRoots {
		for r := range roots {
			mapped, _ := translate(r, effective)
			st.libraryRoots = append(st.libraryRoots, mapped)
		}
	}

	seenServers := knownServers(st)
	seenIdx := knownIndexers(st)
	useProfiles := on(opts.Include.QualityProfiles)
	if snap.radarr != nil {
		tp := &TitlesPreview{AppStatus: appStatus(snap.radarr.Version, snap.radarrErr), RootFolders: []RootFolder{}, Profiles: []ProfileMapping{}, Items: []TitleItem{}}
		if snap.radarrErr == nil {
			tp.RootFolders = describeRoots(radarrRoots, effective, suggested)
			im.planMovies(p, tp, snap.radarr, st, effective, opts.ProfileMapping, useProfiles)
		}
		p.preview.Radarr = tp
	}
	if snap.sonarr != nil {
		tp := &TitlesPreview{AppStatus: appStatus(snap.sonarr.Version, snap.sonarrErr), RootFolders: []RootFolder{}, Profiles: []ProfileMapping{}, Items: []TitleItem{}}
		if snap.sonarrErr == nil {
			tp.RootFolders = describeRoots(sonarrRoots, effective, suggested)
			im.planSeries(ctx, p, tp, snap.sonarr, st, effective, opts.ProfileMapping, useProfiles)
		}
		p.preview.Sonarr = tp
	}
	if snap.prowlarr != nil {
		ip := &IndexersPreview{AppStatus: appStatus(snap.prowlarr.Version, snap.prowlErr), Items: []IndexerItem{}}
		if snap.prowlErr == nil {
			im.planIndexers(ctx, p, ip, snap.prowlarr, seenIdx)
		}
		p.preview.Prowlarr = ip
	}
	if snap.sab != nil {
		sp := &ServersPreview{AppStatus: appStatus(snap.sab.Version, snap.sabErr), Items: []ServerItem{}}
		if snap.sabErr == nil {
			planServers(p, sp, snap.sab, seenServers)
		}
		p.preview.SABnzbd = sp
	}
	if snap.nzbget != nil {
		sp := &ServersPreview{AppStatus: appStatus(snap.nzbget.Version, snap.nzbgetErr), Items: []ServerItem{}}
		if snap.nzbgetErr == nil {
			planNZBGet(p, sp, snap.nzbget, seenServers)
		}
		p.preview.NZBGet = sp
	}
	im.planExtras(ctx, p, &snap.extra, sharedPlan{st: st, maps: effective, suggested: suggested, roots: legacyRoots, servers: seenServers, indexer: seenIdx, src: opts.Sources, opts: opts})
	return p, nil
}

// libraryNote explains a folder outside Mediarium's library folder.
func libraryNote(item *TitleItem, root, what string) {
	if item.Action == ActionSkip || !item.FolderFound || item.InLibraryFolder || root == "" {
		return
	}
	item.Reason += fmt.Sprintf("; the folder is outside Mediarium's %s folder (%s): its files stay where they are, new downloads go to the %s folder", what, root, what)
}

func (im *Importer) planMovies(p *plan, tp *TitlesPreview, d *radarrData, st state, maps []PathMapping, override map[string]int64, useProfiles bool) {
	titles := map[int]int{}
	for _, m := range d.Movies {
		titles[m.QualityProfileID]++
	}
	choice, list := mapProfiles("radarr", d.Profiles, st.profiles, st.defaultName, override, titles)
	tp.Profiles = list
	p.profiles = append(p.profiles, list...)

	for _, m := range d.Movies {
		folder, _ := translate(m.Path, maps)
		item := TitleItem{
			Title: m.Title, Year: m.Year, TMDBID: m.TMDBID, Monitored: m.Monitored, ArrPath: m.Path, Path: folder,
			FolderFound: isDir(folder), InLibraryFolder: insideFolder(folder, st.moviesRoot),
		}
		if m.HasFile {
			item.Files = 1
			if m.MovieFile != nil {
				item.Quality = m.MovieFile.Quality.Quality.Name
			}
		}
		pm := choice.forArr(m.QualityProfileID)
		item.ArrProfile = pm.ArrName
		if useProfiles {
			item.ProfileID, item.ProfileName = pm.ProfileID, pm.ProfileName
		}
		existing, exists := st.movies[m.TMDBID]
		switch {
		case m.TMDBID == 0:
			item.Action, item.Reason = ActionSkip, "no TMDB id"
		case exists && existing.Status == library.StatusDownloaded:
			item.Action, item.Reason = ActionExists, "already in your library"
		case exists && m.HasFile && item.FolderFound:
			item.Action, item.Reason = ActionExists, "already in your library; the file in its folder will be linked"
		case exists && m.HasFile:
			item.Action, item.Reason = ActionExists, "already in your library; folder not found at "+folder+", so its file can't be linked"
		case exists:
			item.Action, item.Reason = ActionExists, "already in your library"
		case m.HasFile && !item.FolderFound:
			item.Action, item.Reason = ActionSkip, "folder not found at "+folder
		case m.HasFile:
			item.Action, item.Reason = ActionAdd, "the file is linked where it is"
		default:
			item.Action, item.Reason = ActionAdd, "Radarr has no file for it yet: added as wanted, no search is started"
		}
		var bad string
		if m.HasFile && item.FolderFound && !(exists && existing.Status == library.StatusDownloaded) {
			bad = titleFolderProblem(folder, st.libraryRoots)
			noteFolderProblem(&item, bad)
		}
		libraryNote(&item, st.moviesRoot, "movies")
		tp.Summary.count(item.Action)
		tp.Items = append(tp.Items, item)
		p.movies = append(p.movies, moviePlan{item: item, movie: m, folder: folder, folderBad: bad})
	}
}

// tmdbForTVDB maps a TVDB id to TMDB's, remembering answers.
func (im *Importer) tmdbForTVDB(ctx context.Context, tvdbID int) (int, error) {
	im.tvdbMu.Lock()
	id, known := im.tvdb[tvdbID]
	im.tvdbMu.Unlock()
	if known {
		return id, nil
	}
	if im.deps.TMDB == nil {
		return 0, errors.New("no TMDB client")
	}
	show, ok, err := im.deps.TMDB().FindShowByTVDBID(ctx, tvdbID)
	if err != nil {
		return 0, err
	}
	if ok {
		id = show.TMDBID
	}
	im.tvdbMu.Lock()
	im.tvdb[tvdbID] = id
	im.tvdbMu.Unlock()
	return id, nil
}

const lookupWorkers = 4

func (im *Importer) planSeries(ctx context.Context, p *plan, tp *TitlesPreview, d *sonarrData, st state, maps []PathMapping, override map[string]int64, useProfiles bool) {
	titles := map[int]int{}
	for _, s := range d.Series {
		titles[s.QualityProfileID]++
	}
	choice, list := mapProfiles("sonarr", d.Profiles, st.profiles, st.defaultTVName, override, titles)
	tp.Profiles = list
	p.profiles = append(p.profiles, list...)

	// Shows without a TMDB id (Sonarr before v4) are looked up by TVDB id.
	tmdbIDs := make([]int, len(d.Series))
	lookupErrs := make([]error, len(d.Series))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, lookupWorkers)
	)
	for i, s := range d.Series {
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

	for i, s := range d.Series {
		folder, _ := translate(s.Path, maps)
		item := TitleItem{
			Title: s.Title, Year: s.Year, TMDBID: tmdbIDs[i], TVDBID: s.TVDBID, Monitored: s.Monitored, ArrPath: s.Path, Path: folder,
			FolderFound: isDir(folder), InLibraryFolder: insideFolder(folder, st.tvRoot), Files: s.Statistics.EpisodeFileCount,
		}
		if s.Monitored {
			for _, season := range s.Seasons {
				if season.SeasonNumber > 0 && !season.Monitored {
					item.UnmonitoredSeasons = append(item.UnmonitoredSeasons, season.SeasonNumber)
				}
			}
			sort.Ints(item.UnmonitoredSeasons)
		}
		pm := choice.forArr(s.QualityProfileID)
		item.ArrProfile = pm.ArrName
		if useProfiles {
			item.ProfileID, item.ProfileName = pm.ProfileID, pm.ProfileName
		}
		hasFiles := s.Statistics.EpisodeFileCount > 0
		_, exists := st.series[item.TMDBID]
		switch {
		case lookupErrs[i] != nil:
			item.Action, item.Reason = ActionSkip, "TMDB lookup failed: "+lookupErrs[i].Error()
		case item.TMDBID == 0 && s.TVDBID > 0:
			item.Action, item.Reason = ActionSkip, fmt.Sprintf("no TMDB id: TMDB has no show with TVDB id %d", s.TVDBID)
		case item.TMDBID == 0:
			item.Action, item.Reason = ActionSkip, "no TMDB id"
		case exists && hasFiles && item.FolderFound:
			item.Action, item.Reason = ActionExists, "already in your library; episodes in its folder not linked yet will be"
		case exists && hasFiles:
			item.Action, item.Reason = ActionExists, "already in your library; folder not found at "+folder+", so its episodes can't be linked"
		case exists:
			item.Action, item.Reason = ActionExists, "already in your library"
		case hasFiles && !item.FolderFound:
			item.Action, item.Reason = ActionSkip, "folder not found at "+folder
		case hasFiles:
			item.Action, item.Reason = ActionAdd, fmt.Sprintf("%d episode file(s) are linked where they are", s.Statistics.EpisodeFileCount)
		default:
			item.Action, item.Reason = ActionAdd, "Sonarr has no episode files for it yet: added, no search is started"
		}
		var bad string
		if hasFiles && item.FolderFound {
			bad = titleFolderProblem(folder, st.libraryRoots)
			noteFolderProblem(&item, bad)
		}
		libraryNote(&item, st.tvRoot, "TV")
		tp.Summary.count(item.Action)
		tp.Items = append(tp.Items, item)
		p.series = append(p.series, seriesPlan{item: item, series: s, folder: folder, folderBad: bad})
	}
}

// normURL compares indexer addresses: lower case, no trailing slash.
func normURL(u string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
}

// newznabBase turns Prowlarr's baseUrl + apiPath (it calls the two joined)
// into the address Mediarium stores (it appends /api itself). ok is false
// for an API path that doesn't end in /api, which Mediarium can't call.
func newznabBase(base, apiPath string) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	apiPath = "/" + strings.Trim(strings.TrimSpace(apiPath), "/")
	switch {
	case apiPath == "/":
		// No API path: Prowlarr calls the base address itself.
		return strings.TrimSuffix(base, "/api"), true
	case apiPath == "/api" || strings.HasSuffix(apiPath, "/api"):
		return base + strings.TrimSuffix(apiPath, "/api"), true
	}
	return base + apiPath, false
}

// seenIndexers are the indexers Mediarium has, or has just planned to add,
// by address (API indexers) and by site definition id.
type seenIndexers struct{ url, def map[string]bool }

func knownIndexers(st state) seenIndexers {
	seen := seenIndexers{url: map[string]bool{}, def: map[string]bool{}}
	for _, inst := range st.indexers {
		if inst.IsCardigann() {
			seen.def[strings.ToLower(inst.DefinitionID)] = true
		} else {
			seen.url[normURL(inst.BaseURL)] = true
		}
	}
	return seen
}

func (im *Importer) planIndexers(ctx context.Context, p *plan, ip *IndexersPreview, d *prowlarrData, seen seenIndexers) {
	seenURL, seenDef := seen.url, seen.def
	for _, x := range d.Indexers {
		ipl := im.planIndexer(ctx, x, d, seenURL, seenDef)
		ip.Summary.count(ipl.item.Action)
		ip.Items = append(ip.Items, ipl.item)
		p.indexers = append(p.indexers, ipl)
	}
}

func (im *Importer) planIndexer(ctx context.Context, x prowlarrIndexer, d *prowlarrData, seenURL, seenDef map[string]bool) indexerPlan {
	item := IndexerItem{Name: x.Name, Implementation: x.Implementation, Protocol: strings.ToLower(x.Protocol), Enabled: x.Enable, Categories: x.categoryIDs()}
	protocol := indexers.ProtocolUsenet
	if item.Protocol == string(indexers.ProtocolTorrent) {
		protocol = indexers.ProtocolTorrent
	}
	ipl := indexerPlan{}
	var notes []string
	if !x.Enable {
		notes = append(notes, "switched off in Prowlarr, so added switched off")
	}

	switch impl := strings.ToLower(x.Implementation); impl {
	case "newznab", "torznab":
		base, _ := x.field("baseUrl")
		apiPath, _ := x.field("apiPath")
		key, _ := x.field("apiKey")
		addr, ok := newznabBase(base, apiPath)
		item.BaseURL = addr
		kind := indexers.KindNewznab
		if impl == "torznab" {
			kind, protocol, item.Protocol = indexers.KindTorznab, indexers.ProtocolTorrent, string(indexers.ProtocolTorrent)
		} else {
			protocol, item.Protocol = indexers.ProtocolUsenet, string(indexers.ProtocolUsenet)
		}
		switch {
		case strings.TrimSpace(base) == "":
			item.Action, item.Reason = ActionSkip, "Prowlarr gave no address for it"
		case !ok:
			if !im.planThroughProwlarr(x, d, &item, &ipl, seenURL, fmt.Sprintf("its API path %q doesn't end in /api, which Mediarium can't call", apiPath)) {
				item.Action, item.Reason = ActionSkip, fmt.Sprintf("its API path %q doesn't end in /api, which Mediarium can't call; add it by hand", apiPath)
			}
		case seenURL[normURL(addr)]:
			item.Action, item.Reason = ActionExists, "already added (same address)"
		default:
			item.Action = ActionAdd
			seenURL[normURL(addr)] = true
			if masked(key) {
				ipl.masked = true
				key = ""
				notes = append(notes, "Prowlarr hid its API key: added switched off, enter the key under Settings > Finding releases > Indexers & Search")
			}
			ipl.inst = indexers.Instance{Name: x.Name, Kind: kind, BaseURL: addr, APIKey: strings.TrimSpace(key), Protocol: protocol, Enabled: x.Enable && !ipl.masked, Priority: x.Priority}
		}
	default:
		im.planSiteIndexer(ctx, x, d, &item, &ipl, &notes, seenURL, seenDef)
	}
	if item.Action == ActionAdd && len(notes) > 0 {
		if item.Reason != "" {
			notes = append([]string{item.Reason}, notes...)
		}
		item.Reason = strings.Join(notes, "; ")
	}
	if item.Action == ActionAdd && ipl.inst.Protocol == indexers.ProtocolTorrent {
		ipl.inst.MinSeeders = x.minSeeders()
	}
	ipl.item = item
	skipBadIndexer(&ipl)
	return ipl
}

// indexerProblem checks an indexer an import would add with the same rules as
// adding one by hand: a name of sane length, an http or https address with no
// login in it, a key and settings that look like ones. "" when it's fine.
// The address itself is never repeated, as it may carry a key.
func indexerProblem(inst indexers.Instance) string {
	if utf8.RuneCountInString(inst.Name) > 80 || strings.IndexFunc(inst.Name, unicode.IsControl) >= 0 {
		return "its name is too long or has hidden characters in it"
	}
	addr := strings.TrimSpace(inst.BaseURL)
	if addr == "" || len(addr) > 2000 || strings.IndexFunc(addr, unicode.IsControl) >= 0 ||
		inputcheck.HTTPURL(addr, "https://api.example.com", true) != "" {
		return "its address isn't a usable http or https web address"
	}
	if u, err := url.Parse(addr); err != nil || u.User != nil {
		return "its address has a username or password in it, which Mediarium doesn't send"
	}
	if inputcheck.APIKey(inst.APIKey) != "" {
		return "its API key doesn't look like one"
	}
	for name, v := range inst.Settings {
		if strings.ContainsRune(v, 0) || len(v) > 8000 {
			return fmt.Sprintf("its setting %q is too long or has hidden characters in it", name)
		}
	}
	return ""
}

// skipBadIndexer turns an indexer that would be added but fails
// indexerProblem into a skip with the reason.
func skipBadIndexer(ipl *indexerPlan) {
	if ipl.item.Action != ActionAdd {
		return
	}
	if problem := indexerProblem(ipl.inst); problem != "" {
		ipl.item.Action, ipl.item.Reason = ActionSkip, problem+": add it by hand"
	}
}

// planSiteIndexer maps an indexer Prowlarr runs from a community definition
// (or one of its own built-in ones) to Mediarium's definition of the same
// id, copying the settings that fit.
func (im *Importer) planSiteIndexer(ctx context.Context, x prowlarrIndexer, d *prowlarrData, item *IndexerItem, ipl *indexerPlan, notes *[]string, seenURL, seenDef map[string]bool) {
	// Whatever Mediarium can't run itself stays on Prowlarr: its own feed for
	// the indexer works the same for every site.
	through := func(why, fallback string) {
		if !im.planThroughProwlarr(x, d, item, ipl, seenURL, why) {
			item.Action, item.Reason = ActionSkip, fallback
		}
	}
	defID := strings.TrimSpace(x.DefinitionName)
	if defID == "" {
		defID = strings.TrimSpace(x.Implementation)
	}
	if im.deps.Definition == nil || defID == "" {
		through("it is an unsupported type ("+x.Implementation+")", "unsupported indexer type ("+x.Implementation+"): add it by hand")
		return
	}
	sum, err := im.deps.Definition(ctx, defID)
	if errors.Is(err, indexers.ErrDefinitionNotFound) && strings.ToLower(defID) != defID {
		sum, err = im.deps.Definition(ctx, strings.ToLower(defID))
	}
	switch {
	case errors.Is(err, indexers.ErrDefinitionNotFound):
		through(fmt.Sprintf("Mediarium's site list has no %q", defID), fmt.Sprintf("Mediarium's site list has no %q. Add it by hand", defID))
		return
	case err != nil:
		item.Action, item.Reason = ActionSkip, "couldn't load Mediarium's site list: "+err.Error()
		return
	case !sum.Supported:
		through(fmt.Sprintf("Mediarium can't run the %s definition yet (%s)", sum.Name, sum.Problem), fmt.Sprintf("Mediarium can't run the %s definition yet (%s)", sum.Name, sum.Problem))
		return
	}
	item.DefinitionID = sum.ID
	if seenDef[strings.ToLower(sum.ID)] {
		item.Action, item.Reason = ActionExists, "already added (same site)"
		return
	}
	protocol := indexers.ProtocolTorrent
	if sum.Protocol == string(indexers.ProtocolUsenet) {
		protocol = indexers.ProtocolUsenet
	}
	item.Protocol = string(protocol)

	base, _ := x.field("baseUrl")
	switch {
	case base != "" && sum.HasLink(base):
		base = strings.TrimSpace(base)
	case len(sum.Links) > 0:
		if base != "" {
			*notes = append(*notes, fmt.Sprintf("its address %s isn't one Mediarium's definition lists, so %s is used", base, sum.Links[0]))
		}
		base = sum.Links[0]
	default:
		through("the definition lists no address", "the definition lists no address: add it by hand")
		return
	}
	item.BaseURL = base

	settings := map[string]string{}
	hidden := false
	for _, s := range sum.Settings {
		if strings.HasPrefix(s.Type, "info") {
			continue
		}
		raw, ok := x.field(s.Name)
		if !ok {
			continue
		}
		if sum.IsSecret(s.Name) && masked(raw) {
			hidden = true
			continue
		}
		v, ok := siteSetting(s, raw)
		if !ok {
			*notes = append(*notes, fmt.Sprintf("setting %q (%s) couldn't be copied; its default is used", s.Name, raw))
			continue
		}
		settings[s.Name] = v
	}
	if hidden {
		ipl.masked = true
		*notes = append(*notes, "Prowlarr hid its password or cookie: added switched off, enter it under Settings > Finding releases > Indexers & Search")
	}
	seenDef[strings.ToLower(sum.ID)] = true
	item.Action = ActionAdd
	ipl.inst = indexers.Instance{Name: x.Name, Kind: indexers.KindCardigann, DefinitionID: sum.ID, BaseURL: base, Protocol: protocol, Enabled: x.Enable && !hidden, Settings: settings, Priority: x.Priority}
	ipl.secret = sum.IsSecret
}

// planThroughProwlarr adds an indexer as a Newznab or Torznab feed served by
// Prowlarr itself, with Prowlarr's API key. It returns false when that isn't
// possible (no id or key), so the caller can skip it with its own reason.
func (im *Importer) planThroughProwlarr(x prowlarrIndexer, d *prowlarrData, item *IndexerItem, ipl *indexerPlan, seenURL map[string]bool, why string) bool {
	if d == nil || x.ID <= 0 || d.Base == "" || d.Key == "" {
		return false
	}
	addr := d.feedURL(x.ID)
	protocol, kind := indexers.ProtocolUsenet, indexers.KindNewznab
	if strings.EqualFold(x.Protocol, string(indexers.ProtocolTorrent)) {
		protocol, kind = indexers.ProtocolTorrent, indexers.KindTorznab
	}
	item.Protocol, item.BaseURL, item.DefinitionID = string(protocol), addr, ""
	if seenURL[normURL(addr)] {
		item.Action, item.Reason = ActionExists, "already added (same address)"
		return true
	}
	seenURL[normURL(addr)] = true
	item.Action = ActionAdd
	item.Reason = why + ", so it stays on Prowlarr: added as Prowlarr's own feed for it, with Prowlarr's API key"
	ipl.inst = indexers.Instance{Name: x.Name, Kind: kind, BaseURL: addr, APIKey: d.Key, Protocol: protocol, Enabled: x.Enable, Priority: x.Priority}
	return true
}

// siteSetting converts one Prowlarr value to the text Mediarium stores for
// a definition setting. Prowlarr keeps a select's choice as its position in
// the option list and a checkbox as true/false.
func siteSetting(s indexers.SettingSummary, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	switch s.Type {
	case "checkbox":
		switch strings.ToLower(raw) {
		case "true", "1", "on", "yes":
			return "true", true
		case "false", "0", "off", "no", "":
			return "false", true
		}
		return "", false
	case "select":
		for _, o := range s.Options {
			if o.Value == raw {
				return raw, true
			}
		}
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n < len(s.Options) {
			return s.Options[n].Value, true
		}
		return "", raw == ""
	}
	return raw, true
}

// newsServer is a Usenet server as an app reports it (SABnzbd, NZBGet).
type newsServer struct {
	Name        string
	Host        string
	Port        int
	SSL         bool
	Username    string
	Password    string
	Connections int
	Priority    int
	Optional    bool
	Enabled     bool
}

type serverKey struct{ host, user string }

// knownServers are the Usenet servers Mediarium already has, by host and
// username.
func knownServers(st state) map[serverKey]bool {
	seen := map[serverKey]bool{}
	for _, sc := range st.servers {
		seen[serverKey{strings.ToLower(strings.TrimSpace(sc.Config.Host)), sc.Config.Username}] = true
	}
	return seen
}

func planServers(p *plan, sp *ServersPreview, d *sabData, seen map[serverKey]bool) {
	cfg := d.Config.Config
	sp.CompleteDir = cfg.Misc.CompleteDir
	for _, c := range cfg.Categories {
		if c.Name != "" && c.Name != "*" {
			sp.Categories = append(sp.Categories, c.Name)
		}
	}
	for _, s := range cfg.Servers {
		name := strings.TrimSpace(s.DisplayName)
		if name == "" {
			name = strings.TrimSpace(s.Name)
		}
		planServer(p, sp, "SABnzbd", newsServer{
			Name: name, Host: strings.TrimSpace(s.Host), Port: int(s.Port), SSL: s.SSL != 0, Username: s.Username,
			Password: s.Password, Connections: int(s.Connections), Priority: int(s.Priority), Optional: s.Optional != 0, Enabled: s.Enable != 0,
		}, seen)
	}
}

// planServer plans one Usenet server of app (SABnzbd or NZBGet): skipped
// without a usable host, "exists" when Mediarium has the same host and
// username, otherwise added (switched off when the app had it off or hid its
// password).
func planServer(p *plan, sp *ServersPreview, app string, s newsServer, seen map[serverKey]bool) {
	host := strings.TrimSpace(s.Host)
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = host
	}
	item := ServerItem{
		Name: name, Host: host, Port: s.Port, SSL: s.SSL, Username: s.Username,
		HasPassword: s.Password != "" && !masked(s.Password), Connections: s.Connections,
		Priority: min(max(s.Priority, 0), 99), Optional: s.Optional, Enabled: s.Enabled,
	}
	if item.Port == 0 {
		item.Port = 119
		if item.SSL {
			item.Port = 563
		}
	}
	if item.Connections <= 0 {
		item.Connections = 8
	}
	item.Connections = min(item.Connections, 100)
	k := serverKey{strings.ToLower(host), s.Username}
	var notes []string
	switch {
	case host == "" || inputcheck.Host(host, "news.example.com") != "":
		item.Action, item.Reason = ActionSkip, "no usable host name"
	case item.Port < 1 || item.Port > 65535:
		item.Action, item.Reason = ActionSkip, fmt.Sprintf("its port %d isn't a number between 1 and 65535", item.Port)
	case seen[k]:
		item.Action, item.Reason = ActionExists, "already added (same host and username)"
	default:
		item.Action = ActionAdd
		seen[k] = true
		if !item.Enabled {
			notes = append(notes, "switched off in "+app+", so added switched off")
		}
		if masked(s.Password) {
			notes = append(notes, app+" did not reveal the password: added switched off, enter it under Settings > Downloading > Usenet")
		}
		if item.Optional {
			notes = append(notes, "optional in "+app+"; Mediarium tries servers in priority order")
		}
		item.Reason = strings.Join(notes, "; ")
	}
	password := s.Password
	if masked(password) {
		password = ""
	}
	sc := download.StoredClient{
		Name: name, Priority: item.Priority, Enabled: item.Enabled && !masked(s.Password),
		Config: download.ClientConfig{Host: host, Port: item.Port, UseSSL: item.SSL, Username: s.Username, Password: password, Connections: item.Connections},
	}
	sp.Summary.count(item.Action)
	sp.Items = append(sp.Items, item)
	p.servers = append(p.servers, serverPlan{item: item, sc: sc, app: app})
}

// arrFileName is the base name of the file Radarr has for a movie.
func arrFileName(m arrMovie) string {
	if m.MovieFile == nil {
		return ""
	}
	if m.MovieFile.RelativePath != "" {
		return filepath.Base(filepath.FromSlash(strings.ReplaceAll(m.MovieFile.RelativePath, `\`, "/")))
	}
	return path.Base(arrPath(m.MovieFile.Path))
}
