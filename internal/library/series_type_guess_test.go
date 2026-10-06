package library

import "testing"

func TestGuessSeriesType(t *testing.T) {
	tests := []struct {
		name      string
		ids       []int
		names     []string
		countries []string
		kind      string
		want      string
	}{
		{"Japanese animation", []int{16, 10759}, nil, []string{"JP"}, "Scripted", SeriesAnime},
		{"named genre", nil, []string{"Animation"}, []string{"jp"}, "Scripted", SeriesAnime},
		{"animation from elsewhere", []int{16}, nil, []string{"US"}, "Scripted", SeriesStandard},
		{"talk show", []int{10767}, nil, []string{"US"}, "Talk Show", SeriesDaily},
		{"news", nil, nil, nil, "news", SeriesDaily},
		{"drama", []int{18}, nil, []string{"GB"}, "Scripted", SeriesStandard},
	}
	for _, tt := range tests {
		if got := GuessSeriesType(tt.ids, tt.names, tt.countries, tt.kind); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}
