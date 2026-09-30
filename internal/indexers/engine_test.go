package indexers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestSearchAllToleratesOneBadIndexer(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureRSS))
	}))
	defer good.Close()

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	instances := []indexers.Instance{
		{ID: 1, Name: "Good Indexer", BaseURL: good.URL, APIKey: "k", Enabled: true},
		{ID: 2, Name: "Bad Indexer", BaseURL: bad.URL, APIKey: "k", Enabled: true},
		{ID: 3, Name: "Disabled Indexer", BaseURL: good.URL, APIKey: "k", Enabled: false},
	}

	outcomes := indexers.SearchAll(context.Background(), instances, "the matrix", []int{2000})
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 outcomes (disabled indexer skipped), got %d", len(outcomes))
	}

	var goodOutcome, badOutcome *indexers.Outcome
	for i := range outcomes {
		switch outcomes[i].IndexerName {
		case "Good Indexer":
			goodOutcome = &outcomes[i]
		case "Bad Indexer":
			badOutcome = &outcomes[i]
		}
	}
	if goodOutcome == nil || goodOutcome.Err != nil || len(goodOutcome.Results) != 1 {
		t.Fatalf("expected good indexer to succeed with 1 result, got %+v", goodOutcome)
	}
	if badOutcome == nil || badOutcome.Err == nil {
		t.Fatalf("expected bad indexer to have an error, got %+v", badOutcome)
	}

	merged := indexers.MergeResults(outcomes)
	if len(merged) != 1 {
		t.Fatalf("expected merged results to only include the good indexer's result, got %d", len(merged))
	}
}
