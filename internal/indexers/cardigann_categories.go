package indexers

import (
	"strconv"
	"strings"
)

// newznabCategories is the standard Newznab category table that definition
// category mappings refer to by name.
var newznabCategories = map[string]int{
	"console": 1000, "console/nds": 1010, "console/psp": 1020, "console/wii": 1030, "console/xbox": 1040,
	"console/xbox 360": 1050, "console/wiiware": 1060, "console/xbox 360 dlc": 1070, "console/ps3": 1080,
	"console/other": 1090, "console/3ds": 1110, "console/ps vita": 1120, "console/wiiu": 1130,
	"console/xbox one": 1140, "console/ps4": 1180,
	"movies": 2000, "movies/foreign": 2010, "movies/other": 2020, "movies/sd": 2030, "movies/hd": 2040,
	"movies/uhd": 2045, "movies/bluray": 2050, "movies/3d": 2060, "movies/dvd": 2070, "movies/web-dl": 2080,
	"movies/x265": 2090,
	"audio":       3000, "audio/mp3": 3010, "audio/video": 3020, "audio/audiobook": 3030, "audio/lossless": 3040,
	"audio/other": 3050, "audio/foreign": 3060,
	"pc": 4000, "pc/0day": 4010, "pc/iso": 4020, "pc/mac": 4030, "pc/mobile-other": 4040, "pc/games": 4050,
	"pc/mobile-ios": 4060, "pc/mobile-android": 4070,
	"tv": 5000, "tv/web-dl": 5010, "tv/foreign": 5020, "tv/sd": 5030, "tv/hd": 5040, "tv/uhd": 5045,
	"tv/other": 5050, "tv/sport": 5060, "tv/anime": 5070, "tv/documentary": 5080, "tv/x265": 5090,
	"xxx": 6000, "xxx/dvd": 6010, "xxx/wmv": 6020, "xxx/xvid": 6030, "xxx/x264": 6040, "xxx/uhd": 6045,
	"xxx/pack": 6050, "xxx/imageset": 6060, "xxx/other": 6070, "xxx/sd": 6080, "xxx/web-dl": 6090,
	"books": 7000, "books/mags": 7010, "books/ebook": 7020, "books/comics": 7030, "books/technical": 7040,
	"books/other": 7050, "books/foreign": 7060,
	"other": 8000, "other/misc": 8010, "other/hashed": 8020,
}

func newznabID(name string) int {
	name = strings.ToLower(strings.TrimSpace(name))
	if id, ok := newznabCategories[name]; ok {
		return id
	}
	if n, err := strconv.Atoi(name); err == nil {
		return n
	}
	return 0
}

type catMapping struct {
	site    string
	newznab int
	desc    string
	def     bool
}

func (d *Definition) mappings() []catMapping {
	var out []catMapping
	for _, m := range d.Caps.CategoryMappings {
		out = append(out, catMapping{site: m.ID, newznab: newznabID(m.Cat), desc: m.Desc, def: m.Default})
	}
	for id, cat := range d.Caps.Categories {
		out = append(out, catMapping{site: id, newznab: newznabID(cat), desc: cat})
	}
	return out
}

// expandCategories adds every sub-category of a requested parent category
// (2000 also means 2010, 2040...).
func expandCategories(cats []int) map[int]bool {
	want := map[int]bool{}
	for _, c := range cats {
		want[c] = true
		if c%1000 == 0 {
			for _, id := range newznabCategories {
				if id/1000 == c/1000 {
					want[id] = true
				}
			}
		}
	}
	return want
}

// siteCategories maps requested Newznab categories to the site's own
// category ids (for {{ .Categories }}); with none requested, the
// definition's default categories.
func (d *Definition) siteCategories(cats []int) []string {
	var out []string
	seen := map[string]bool{}
	want := expandCategories(cats)
	for _, m := range d.mappings() {
		ok := want[m.newznab]
		if len(cats) == 0 {
			ok = m.def
		}
		if ok && m.newznab != 0 && !seen[m.site] {
			seen[m.site] = true
			out = append(out, m.site)
		}
	}
	return out
}

// newznabForSite maps a site category id to Newznab categories.
func (d *Definition) newznabForSite(site string) []int {
	site = strings.TrimSpace(site)
	var out []int
	for _, m := range d.mappings() {
		if m.site == site && m.newznab != 0 {
			out = appendUnique(out, m.newznab)
		}
	}
	return out
}

// newznabForDesc maps a site category name to Newznab categories.
func (d *Definition) newznabForDesc(desc string) []int {
	desc = strings.TrimSpace(desc)
	var out []int
	for _, m := range d.mappings() {
		if strings.EqualFold(m.desc, desc) && m.newznab != 0 {
			out = appendUnique(out, m.newznab)
		}
	}
	return out
}

func appendUnique(list []int, add ...int) []int {
	for _, a := range add {
		dup := false
		for _, x := range list {
			if x == a {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, a)
		}
	}
	return list
}

// filterByCategory drops results the site filed under a category that
// wasn't asked for (sites often ignore the category filter). Results with
// no category are kept.
func filterByCategory(results []Result, cats []int) []Result {
	if len(cats) == 0 {
		return results
	}
	want := expandCategories(cats)
	out := results[:0]
	for _, r := range results {
		keep := len(r.Categories) == 0
		for _, c := range r.Categories {
			if want[c] || want[c/1000*1000] {
				keep = true
				break
			}
		}
		if keep {
			out = append(out, r)
		}
	}
	return out
}
