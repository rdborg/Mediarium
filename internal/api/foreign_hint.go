package api

import (
	"strings"

	"github.com/rdborg/mediarium/internal/library"
)

// countryNames are the countries a hint can name. A show from an English-speaking
// country never gets one.
var countryNames = map[string]string{
	"FR": "France", "DE": "Germany", "ES": "Spain", "IT": "Italy", "NL": "the Netherlands", "PL": "Poland",
	"PT": "Portugal", "BR": "Brazil", "RU": "Russia", "UA": "Ukraine", "JP": "Japan", "KR": "South Korea",
	"CN": "China", "TW": "Taiwan", "TR": "Turkey", "SE": "Sweden", "NO": "Norway", "DK": "Denmark",
	"FI": "Finland", "BE": "Belgium", "CH": "Switzerland", "AT": "Austria", "MX": "Mexico", "AR": "Argentina",
	"IN": "India", "TH": "Thailand", "GR": "Greece", "CZ": "Czechia", "HU": "Hungary", "RO": "Romania",
}

// foreignShowHint explains a search that found nothing for a show made outside the
// English-speaking world: most indexers list mainly English releases, so the show may
// simply not be there under any name. Empty when the show gives no reason to say so.
func foreignShowHint(sr library.Series) string {
	var names []string
	for _, c := range sr.OriginCountry {
		c = strings.ToUpper(strings.TrimSpace(c))
		switch c {
		case "US", "GB", "CA", "AU", "NZ", "IE":
			return "" // also made in an English-speaking country: the usual search should find it
		}
		if n, ok := countryNames[c]; ok {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "This show is from " + names[0] + ", and many indexers carry mostly English releases, so it may not be on yours in any language. An indexer that lists releases from there may have it, or search by hand with the original title or a language word such as FRENCH or MULTI."
}
