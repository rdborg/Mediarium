package indexers

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Protocol distinguishes Usenet (Newznab) from torrent (Torznab) indexers.
// Both speak the same underlying XML API shape (see the package doc
// comment in newznab.go); this only changes how results
// are tagged/handled downstream (search UI protocol icon, which download
// engine a grab uses).
type Protocol string

const (
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

// Kind says how an indexer is searched.
type Kind string

const (
	KindNewznab   Kind = "newznab"   // Newznab API (Usenet)
	KindTorznab   Kind = "torznab"   // Torznab API (torrents, e.g. Prowlarr/Jackett)
	KindCardigann Kind = "cardigann" // a site driven by a community definition
)

// Instance is one user-configured indexer: a Newznab/Torznab API (base URL
// + API key), or a definition-based site (definition id + the settings the
// definition asks for).
type Instance struct {
	ID           int64
	Name         string
	Kind         Kind
	DefinitionID string
	BaseURL      string
	APIKey       string
	Protocol     Protocol
	Enabled      bool
	// Settings are a definition-based indexer's values (secrets included,
	// decrypted); nil for API indexers.
	Settings map[string]string
	// Cardigann runs definition-based indexers; the repo sets it.
	Cardigann *CardigannManager
	// LastTestError is the message of the last failed Test ("" = passed or
	// never tested); LastTestAt is when it ran.
	LastTestError string
	LastTestAt    time.Time
}

// IsCardigann reports whether the instance is a definition-based site.
func (inst Instance) IsCardigann() bool { return inst.Kind == KindCardigann }

// Outcome is one indexer's contribution to an aggregated search — Err is
// set instead of aborting the whole search, so a single bad/slow indexer
// never stalls the others.
type Outcome struct {
	IndexerName string
	Results     []Result
	Err         error
}

const perIndexerTimeout = 15 * time.Second

// SearchAll queries every enabled instance concurrently and returns one
// Outcome per instance ("one unified result list ... clearly
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
			timeout := perIndexerTimeout
			if inst.IsCardigann() {
				timeout = cardigannTimeout
			}
			reqCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			var (
				results []Result
				err     error
			)
			if inst.IsCardigann() {
				if inst.Cardigann == nil {
					err = fmt.Errorf("%s: definition-based indexers are not available", inst.Name)
				} else {
					results, err = inst.Cardigann.Search(reqCtx, inst, query, categories)
				}
			} else {
				client := NewNewznabClient(inst.Name, inst.BaseURL, inst.APIKey)
				results, err = client.Search(reqCtx, query, categories)
			}
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
