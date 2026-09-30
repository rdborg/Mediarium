package migrate

import (
	"context"
	"sync"
)

// extraData is what was read from the apps added after the first four
// (Jackett, NZBHydra2, ...), so the snapshot and plan code in plan.go stays
// short. Each app has its own file.
type extraData struct {
	jackett      *jackettData
	jackettErr   error
	hydra        *hydraData
	hydraErr     error
	bazarr       *bazarrData
	bazarrErr    error
	overseerr    *overseerrData
	overseerrErr error
	ombi         *ombiData
	ombiErr      error
	medusa       []legacyShow
	medusaErr    error
	medusaOn     bool
	sickchill    []legacyShow
	sickchillErr error
	sickchillOn  bool
}

// appErr is an app that could not be read.
type appErr struct {
	app string
	err error
}

func (e *extraData) errs() []appErr {
	return []appErr{{"Jackett", e.jackettErr}, {"NZBHydra2", e.hydraErr}, {"Overseerr", e.overseerrErr}, {"Ombi", e.ombiErr}, {"Bazarr", e.bazarrErr}, {"Medusa", e.medusaErr}, {"SickChill", e.sickchillErr}}
}

// fetchExtras starts reading each requested extra app.
func (im *Importer) fetchExtras(ctx context.Context, src Sources, e *extraData, wg *sync.WaitGroup) {
	if src.Jackett != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchJackett(ctx, im.hc, *src.Jackett)
			e.jackett, e.jackettErr = &d, err
		}()
	}
	if src.NZBHydra != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchNZBHydra(ctx, im.hc, *src.NZBHydra)
			e.hydra, e.hydraErr = &d, err
		}()
	}
	if src.Medusa != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.medusa, e.medusaErr = fetchMedusa(ctx, im.hc, *src.Medusa)
			e.medusaOn = true
		}()
	}
	if src.SickChill != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.sickchill, e.sickchillErr = fetchSickChill(ctx, im.hc, *src.SickChill)
			e.sickchillOn = true
		}()
	}
	if src.Bazarr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchBazarr(ctx, im.hc, *src.Bazarr)
			e.bazarr, e.bazarrErr = &d, err
		}()
	}
	if src.Overseerr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchOverseerr(ctx, im.hc, *src.Overseerr)
			e.overseerr, e.overseerrErr = &d, err
		}()
	}
	if src.Ombi != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := fetchOmbi(ctx, im.hc, *src.Ombi)
			e.ombi, e.ombiErr = &d, err
		}()
	}
}

// gateExtras drops the apps whose part of the import is switched off, so
// they are not even read.
func gateExtras(src *Sources, inc Include) {
	if !on(inc.Indexers) {
		src.Jackett, src.NZBHydra = nil, nil
	}
	if !on(inc.Series) {
		src.Medusa, src.SickChill = nil, nil
	}
	if !optIn(inc.SubtitleLanguages) {
		src.Bazarr = nil
	}
	if !on(inc.Requests) {
		src.Overseerr, src.Ombi = nil, nil
	}
}

// sharedPlan is what the extra apps' planning shares with the first four:
// the Mediarium state and the servers and indexers already planned.
type sharedPlan struct {
	st        state
	maps      []PathMapping
	suggested []PathMapping
	roots     map[string]map[string][]string
	servers   map[serverKey]bool
	indexer   seenIndexers
	src       Sources
	opts      Options
}

// planExtras adds the extra apps' parts to the plan and the preview.
func (im *Importer) planExtras(ctx context.Context, p *plan, e *extraData, sh sharedPlan) {
	if e.jackett != nil {
		ip := &IndexersPreview{AppStatus: appStatus(e.jackett.Version, e.jackettErr), Items: []IndexerItem{}}
		if e.jackettErr == nil {
			im.planJackett(ctx, p, ip, e.jackett, sh.src.Jackett.Direct, sh.indexer)
		}
		p.preview.Jackett = ip
	}
	if e.hydra != nil {
		ip := &IndexersPreview{AppStatus: appStatus("", e.hydraErr), Items: []IndexerItem{}}
		if e.hydraErr == nil {
			im.planNZBHydra(p, ip, e.hydra, sh.indexer)
		}
		p.preview.NZBHydra = ip
	}
	if e.medusaOn {
		tp := newTitlesPreview("", e.medusaErr)
		if e.medusaErr == nil {
			tp.RootFolders = describeRoots(sh.roots["Medusa"], sh.maps, sh.suggested)
			im.planLegacyShows(ctx, p, tp, "Medusa", e.medusa, sh.st, sh.maps)
		}
		p.preview.Medusa = tp
	}
	if e.sickchillOn {
		tp := newTitlesPreview("", e.sickchillErr)
		if e.sickchillErr == nil {
			tp.RootFolders = describeRoots(sh.roots["SickChill"], sh.maps, sh.suggested)
			im.planLegacyShows(ctx, p, tp, "SickChill", e.sickchill, sh.st, sh.maps)
		}
		p.preview.SickChill = tp
	}
	if e.bazarr != nil {
		sp := &SubtitlesPreview{AppStatus: appStatus(e.bazarr.Version, e.bazarrErr), Languages: []SubtitleLanguage{}, Current: []string{}, New: []string{}, Providers: []SubtitleProvider{}}
		if e.bazarrErr == nil {
			im.planBazarr(p, sp, e.bazarr, optIn(sh.opts.Include.SubtitleLanguages))
		}
		p.preview.Bazarr = sp
	}
	seenRequests := map[string]bool{}
	if e.overseerr != nil {
		rp := &RequestsPreview{AppStatus: appStatus(e.overseerr.Version, e.overseerrErr), Items: []RequestItem{}}
		if e.overseerrErr == nil {
			im.planRequests(ctx, p, rp, "Overseerr", e.overseerr.entries(), sh.st, seenRequests)
		}
		p.preview.Overseerr = rp
	}
	if e.ombi != nil {
		rp := &RequestsPreview{AppStatus: appStatus("", e.ombiErr), Items: []RequestItem{}}
		if e.ombiErr == nil {
			im.planRequests(ctx, p, rp, "Ombi", e.ombi.entries(), sh.st, seenRequests)
		}
		p.preview.Ombi = rp
	}
}
