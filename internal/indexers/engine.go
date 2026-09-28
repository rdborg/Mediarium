package indexers

import (
	"context"
	"sync"
	"time"
)

// Protocol distinguishes Usenet (Newznab) from torrent (Torznab) indexers
// (PRD.md §7 Phase 2). Both speak the same underlying XML API shape (see
// the package doc comment in newznab.go); this only changes how results
// are tagged/handled downstream (search UI protocol icon, which download
// engine a grab uses).
type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

// Instance is one user-configured indexer (a Definition + the user's own
// base URL/API key for it).
type Instance struct {
	ID           int64
	Name         string
	DefinitionID string
	BaseURL      string
	APIKey       string
	Protocol     Protocol
	Enabled      bool
}

// Outcome is one indexer's contribution to an aggregated search — Err is
// set instead of aborting the whole search, so a single bad/slow indexer
// never stalls the others (PRD.md §11).
type Outcome struct {
	IndexerName string
	Results     []Result
	Err         error
}

const perIndexerTimeout = 15 * time.Second

// SearchAll queries every enabled instance concurrently and returns one
// Outcome per instance (PRD §6 — "one unified result list ... clearly
// tagged by source").
func SearchAll(ctx context.Context, instances []Instance, query string, categories []int) []Outcome {
	var enabled []Instance
	for _, inst := range instances {
		if inst.Enabled {
			enabled = append(enabled, inst)
		}
	}

	outcomes := make([]Outcome, len(enabled))
	var wg sync.WaitGroup
	for i, inst := range enabled {
		wg.Add(1)
		go func(i int, inst Instance) {
			defer wg.Done()
			reqCtx, cancel := context.WithTimeout(ctx, perIndexerTimeout)
			defer cancel()

			client := NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey)
			results, err := client.Search(reqCtx, query, categories)
			for j := range results {
				results[j].Protocol = inst.Protocol
			}
			outcomes[i] = Outcome{IndexerName: inst.Name, Results: results, Err: err}
		}(i, inst)
	}
	wg.Wait()
	return outcomes
}

// MergeResults flattens successful outcomes into a single slice, ignoring
// (but not discarding — callers can still inspect Outcome.Err) failed ones.
func MergeResults(outcomes []Outcome) []Result {
	var all []Result
	for _, o := range outcomes {
		if o.Err == nil {
			all = append(all, o.Results...)
		}
	}
	return all
}
