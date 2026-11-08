package requests

import (
	"strconv"
	"strings"
)

// An age limit says how mature a title may be for an account. Certifications
// differ from country to country, so they are mapped onto a short ordered
// scale; a title is allowed when its level is not above the account's.
const (
	LevelAll    = 0 // everyone: G, TV-Y, TV-G, U, 0
	LevelKids   = 1 // young children up to about 10: PG, TV-PG, TV-Y7, 6, 9
	LevelTeens  = 2 // about 12 to 14: PG-13, TV-14, 12, 12A, 14
	LevelMature = 3 // about 15 to 17: R, TV-MA, 15, 16, 17
	LevelAdult  = 4 // 18 and over: NC-17, 18, R18
)

// certLevels maps certifications to levels. Keys are upper case with spaces
// and dashes written as TMDB writes them.
var certLevels = map[string]int{
	// United States, films (MPAA) and television.
	"G": LevelAll, "TV-Y": LevelAll, "TV-G": LevelAll,
	"PG": LevelKids, "TV-PG": LevelKids, "TV-Y7": LevelKids, "TV-Y7-FV": LevelKids,
	"PG-13": LevelTeens, "TV-14": LevelTeens,
	"R": LevelMature, "TV-MA": LevelMature,
	"NC-17": LevelAdult,
	// United Kingdom and Ireland.
	"U": LevelAll, "UC": LevelAll, "12A": LevelTeens, "R18": LevelAdult,
	// Germany, France, Netherlands, Spain and others that use plain ages.
	"AL": LevelAll, "TP": LevelAll, "ALL": LevelAll,
	"VM14": LevelTeens, "VM18": LevelAdult,
	"X": LevelAdult, "XXX": LevelAdult,
}

// unrated are the words for a title that has no age rating.
var unrated = map[string]bool{"": true, "NR": true, "UR": true, "UNRATED": true, "NOT RATED": true, "NOT YET RATED": true, "N/A": true, "NA": true, "-": true}

// normalizeCert cleans a certification for lookup: "fsk 16", "16+" and "Ab 16"
// all become "16".
func normalizeCert(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	for _, p := range []string{"FSK ", "FSK-", "AB ", "KIJKWIJZER ", "BBFC ", "MPAA ", "RATED "} {
		c = strings.TrimPrefix(c, p)
	}
	c = strings.TrimSuffix(c, "+")
	return strings.TrimSpace(c)
}

// levelOfAge puts a minimum age on the scale.
func levelOfAge(age int) int {
	switch {
	case age <= 0:
		return LevelAll
	case age <= 10:
		return LevelKids
	case age <= 14:
		return LevelTeens
	case age <= 17:
		return LevelMature
	}
	return LevelAdult
}

// RatingLevel returns where a certification sits on the scale. known is false
// for a title with no rating, or one this scale doesn't know.
func RatingLevel(cert string) (level int, known bool) {
	c := normalizeCert(cert)
	if unrated[c] {
		return 0, false
	}
	if l, ok := certLevels[c]; ok {
		return l, true
	}
	if age, err := strconv.Atoi(c); err == nil && age >= 0 && age < 100 {
		return levelOfAge(age), true
	}
	return 0, false
}

// LimitLevel reads an account's limit: a certification ("PG-13", "TV-14") or
// a plain age ("12"). ok is false when it isn't either. An empty limit means
// no limit and also answers ok = false.
func LimitLevel(limit string) (level int, ok bool) {
	if strings.TrimSpace(limit) == "" {
		return 0, false
	}
	return RatingLevel(limit)
}

// AgeAllowed reports whether a title with this certification may be shown or
// asked for by an account with this limit. No limit allows everything. A
// title without a known rating is allowed unless blockUnrated is set.
func AgeAllowed(limit string, blockUnrated bool, cert string) bool {
	max, limited := LimitLevel(limit)
	if !limited {
		return true
	}
	level, known := RatingLevel(cert)
	if !known {
		return !blockUnrated
	}
	return level <= max
}
