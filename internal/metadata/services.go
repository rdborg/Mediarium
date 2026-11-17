package metadata

import (
	"net/url"
	"strconv"
	"strings"
)

// StreamingService is a service people subscribe to, with the TMDB watch
// provider ids that stand for it. TMDB lists a service under more than one
// id (a plan with adverts, a premium plan), so a service has several.
type StreamingService struct {
	Key       string
	Name      string
	Providers []int
}

// streamingServices is the short list Discover offers under "Hide what I
// already have on". The keys are what the API takes and what the browser
// remembers, so they never change; the provider ids can.
var streamingServices = []StreamingService{
	{"netflix", "Netflix", []int{8, 1796}},
	{"prime", "Prime Video", []int{9, 2100}},
	{"disney", "Disney+", []int{337}},
	{"max", "Max (HBO)", []int{1899, 384}},
	{"apple", "Apple TV+", []int{350}},
	{"paramount", "Paramount+", []int{531, 2303, 2616}},
	{"hulu", "Hulu", []int{15}},
	{"peacock", "Peacock", []int{386, 387}},
}

// StreamingServices lists the services Discover can hide, in display order.
func StreamingServices() []StreamingService {
	return append([]StreamingService(nil), streamingServices...)
}

// ParseServices reads a comma separated list of service keys. It returns the
// keys without duplicates, in the order given, and false when one is unknown.
// An empty value is no services.
func ParseServices(csv string) ([]string, bool) {
	var out []string
	seen := map[string]bool{}
	for _, k := range strings.Split(csv, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || seen[k] {
			continue
		}
		known := false
		for _, s := range streamingServices {
			if s.Key == k {
				known = true
			}
		}
		if !known {
			return nil, false
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, true
}

// providerParam joins the provider ids of the given service keys with "|",
// which TMDB reads as "any of these".
func providerParam(keys []string) string {
	var ids []string
	seen := map[int]bool{}
	for _, k := range keys {
		for _, s := range streamingServices {
			if s.Key != k {
				continue
			}
			for _, id := range s.Providers {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, strconv.Itoa(id))
				}
			}
		}
	}
	return strings.Join(ids, "|")
}

// Hiding asks a Discover list to leave out titles that are on some
// streaming services in a region.
type Hiding struct {
	Services []string // keys from StreamingServices
	Region   string   // an ISO 3166 country code; "US" when empty
}

// Active reports whether anything is to be hidden.
func (h Hiding) Active() bool { return providerParam(h.Services) != "" }

// apply adds TMDB's "not available on these providers" filter to a discover query.
func (h Hiding) apply(v url.Values) {
	p := providerParam(h.Services)
	if p == "" {
		return
	}
	v.Set("watch_region", h.region())
	v.Set("without_watch_providers", p)
}
