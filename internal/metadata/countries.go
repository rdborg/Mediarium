package metadata

import "slices"

// ReleaseCountries are the countries offered for release dates (Settings > Library and
// files > Movie and show info): the ISO 3166-1 codes TMDB lists dates under,
// limited to the ones most people live in. The screen writes the names in the
// person's language.
var ReleaseCountries = []string{
	"US", "GB", "CA", "AU", "NZ", "IE", "IN", "DE", "AT", "CH", "FR", "BE", "NL", "LU",
	"ES", "PT", "IT", "MT", "GR", "CY", "SE", "NO", "DK", "FI", "IS", "PL", "CZ", "SK",
	"HU", "RO", "BG", "HR", "SI", "RS", "UA", "RU", "TR", "IL", "AE", "SA", "EG", "ZA",
	"NG", "KE", "MA", "BR", "MX", "AR", "CL", "CO", "PE", "JP", "KR", "CN", "TW", "HK",
	"SG", "MY", "TH", "ID", "PH", "VN", "PK", "BD", "LK",
}

// IsReleaseCountry reports whether code is one of ReleaseCountries.
func IsReleaseCountry(code string) bool {
	return slices.Contains(ReleaseCountries, code)
}
