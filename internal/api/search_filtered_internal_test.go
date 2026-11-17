package api

import (
	"reflect"
	"testing"
)

// The "Searches used" part of a movie, show or book search counts the releases
// left out for another title, any other group left out, and then each reason
// automation would skip a release in the list, most frequent first.
func TestReleaseFiltered(t *testing.T) {
	const profile = "The profile does not take this quality."
	blocked := searchResultPayload{Blocklisted: true}
	blocked.addStateReasons()
	var someIgnored ignoredReleases
	someIgnored.add("Other.Show.S01E01")
	someIgnored.add("Other.Show.S01E02")
	tests := []struct {
		name    string
		ignored ignoredReleases
		list    []searchResultPayload
		extra   []filteredCount
		want    []filteredCount
	}{
		{"nothing set aside", ignoredReleases{}, []searchResultPayload{{}}, nil, nil},
		{"other titles only", someIgnored, nil, nil, []filteredCount{{Reason: "not this title", Count: 2}}},
		{
			"a flag the list did not write out still counts (a book list)",
			ignoredReleases{}, []searchResultPayload{{Blocklisted: true}, {LanguageFit: "other"}, {Blocklisted: true}}, nil,
			[]filteredCount{{Reason: blocked.Reasons[0].Text, Count: 2}, {Reason: "It is in another language than the one you prefer, so automatic searches leave it out.", Count: 1}},
		},
		{
			"a reason written out and its flag are counted once",
			ignoredReleases{}, []searchResultPayload{blocked}, nil,
			[]filteredCount{{Reason: blocked.Reasons[0].Text, Count: 1}},
		},
		{
			"other titles, an extra group and the reasons, most frequent first",
			someIgnored,
			[]searchResultPayload{{Reasons: []releaseReason{{"quality", profile}}}, {Reasons: []releaseReason{{"quality", profile}, {"quality", profile}}}, blocked},
			[]filteredCount{{Reason: notMultiSeasonPack, Count: 3}, {Reason: otherBookFormat}},
			[]filteredCount{{Reason: "not this title", Count: 2}, {Reason: notMultiSeasonPack, Count: 3}, {Reason: profile, Count: 2}, {Reason: blocked.Reasons[0].Text, Count: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseFiltered(tt.ignored, tt.list, tt.extra...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
