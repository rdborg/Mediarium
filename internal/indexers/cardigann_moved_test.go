package indexers

import "testing"

// A site that moves to a new domain keeps working for indexers saved with the
// old address: the definition lists the old one under legacylinks.
func TestCurrentSiteAddress(t *testing.T) {
	def := &Definition{
		Links:       []string{"https://moved.example/", "https://mirror.example/"},
		LegacyLinks: []string{"https://old.example/", "https://older.example"},
	}
	tests := []struct {
		name, saved, want string
	}{
		{"empty stays empty", "", ""},
		{"current first link", "https://moved.example/", "https://moved.example/"},
		{"current mirror is kept", "https://mirror.example", "https://mirror.example"},
		{"legacy address moves to the first link", "https://old.example/", "https://moved.example/"},
		{"legacy without trailing slash", "https://older.example/", "https://moved.example/"},
		{"legacy in other case", "HTTPS://OLD.example", "https://moved.example/"},
		{"unknown address is left alone", "http://192.168.1.5:9117/", "http://192.168.1.5:9117/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentSiteAddress(def, tt.saved); got != tt.want {
				t.Fatalf("currentSiteAddress(%q) = %q, want %q", tt.saved, got, tt.want)
			}
		})
	}
	if got := currentSiteAddress(&Definition{LegacyLinks: []string{"https://old.example/"}}, "https://old.example/"); got != "https://old.example/" {
		t.Fatalf("a definition with no links must not change the address, got %q", got)
	}
}

func TestNewSessionUsesMovedSite(t *testing.T) {
	def := &Definition{ID: "x", Name: "X", Links: []string{"https://new.example/"}, LegacyLinks: []string{"https://old.example/"}}
	s, err := newSession(&CardigannManager{}, def, Instance{Name: "X", BaseURL: "https://old.example/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.base.String(); got != "https://new.example/" {
		t.Fatalf("base = %q, want the new address", got)
	}
}
