package api

import (
	"strings"

	"github.com/rdborg/mediarium/internal/quality"
)

// Radarr and Sonarr know more qualities than Mediarium has: WEBRip next to
// WEBDL, Bluray-480p, Raw-HD, CAM and so on. Recyclarr names them in the
// profiles and size lists of the TRaSH Guides, so they are known here too and
// each one is either mapped onto the Mediarium quality it counts as (WEBRip-1080p
// is WEBDL-1080p here: Mediarium does not tell them apart) or, with no
// counterpart (Raw-HD, BR-DISK), accepted and left out.

// arrCatalogItem is one quality of the real apps.
type arrCatalogItem struct {
	Q arrQualityName
	// Tier is the Mediarium quality it counts as, when HasTier.
	Tier    quality.Tier
	HasTier bool
	// Alias marks a name that shares its Mediarium quality with another one
	// (the "real" one is not an alias). Lists of profiles do not show aliases.
	Alias bool
	// Primary is where it sits in the size list: the first entries are the
	// Mediarium qualities in their usual order, the rest follow.
	order int
}

type arrExtraQuality struct {
	after   quality.Tier
	q       arrQualityName
	tier    quality.Tier
	hasTier bool
}

// arrExtraQualities are the qualities of each app that have no Mediarium one
// of their own, placed after the Mediarium quality they sit next to.
func arrExtraQualities(app arrApp) []arrExtraQuality {
	type q = arrQualityName
	if app == arrSonarr {
		return []arrExtraQuality{
			{quality.TierWebDL480p, q{12, "WEBRip-480p", "webRip", 480, ""}, quality.TierWebDL480p, true},
			{quality.TierWebDL480p, q{13, "Bluray-480p", "bluray", 480, ""}, quality.TierDVD, true},
			{quality.TierWebDL480p, q{22, "Bluray-576p", "bluray", 576, ""}, quality.TierDVD, true},
			{quality.TierWebDL720p, q{14, "WEBRip-720p", "webRip", 720, ""}, quality.TierWebDL720p, true},
			{quality.TierWebDL1080p, q{15, "WEBRip-1080p", "webRip", 1080, ""}, quality.TierWebDL1080p, true},
			{quality.TierRemux1080p, q{10, "Raw-HD", "televisionRaw", 1080, ""}, "", false},
			{quality.TierWebDL2160p, q{17, "WEBRip-2160p", "webRip", 2160, ""}, quality.TierWebDL2160p, true},
		}
	}
	return []arrExtraQuality{
		{quality.TierUnknown, q{24, "WORKPRINT", "workprint", 0, "none"}, quality.TierPreRelease, true},
		{quality.TierUnknown, q{25, "CAM", "cam", 0, "none"}, quality.TierPreRelease, true},
		{quality.TierPreRelease, q{27, "TELECINE", "telecine", 0, "none"}, quality.TierPreRelease, true},
		{quality.TierPreRelease, q{29, "REGIONAL", "dvd", 480, "regional"}, quality.TierPreRelease, true},
		{quality.TierPreRelease, q{28, "DVDSCR", "dvd", 480, "screener"}, quality.TierPreRelease, true},
		{quality.TierDVD, q{23, "DVD-R", "dvd", 480, "remux"}, quality.TierDVD, true},
		{quality.TierWebDL480p, q{12, "WEBRip-480p", "webrip", 480, "none"}, quality.TierWebDL480p, true},
		{quality.TierWebDL480p, q{20, "Bluray-480p", "bluray", 480, "none"}, quality.TierDVD, true},
		{quality.TierWebDL480p, q{21, "Bluray-576p", "bluray", 576, "none"}, quality.TierDVD, true},
		{quality.TierWebDL720p, q{14, "WEBRip-720p", "webrip", 720, "none"}, quality.TierWebDL720p, true},
		{quality.TierWebDL1080p, q{15, "WEBRip-1080p", "webrip", 1080, "none"}, quality.TierWebDL1080p, true},
		{quality.TierRemux1080p, q{10, "Raw-HD", "tv", 1080, "rawhd"}, "", false},
		{quality.TierRemux1080p, q{22, "BR-DISK", "bluray", 1080, "brdisk"}, "", false},
		{quality.TierWebDL2160p, q{17, "WEBRip-2160p", "webrip", 2160, "none"}, quality.TierWebDL2160p, true},
	}
}

// arrCatalog lists every quality of an app, worst first: each Mediarium
// quality, with the others after the one they sit next to.
func arrCatalog(app arrApp) []arrCatalogItem {
	extras := arrExtraQualities(app)
	var out []arrCatalogItem
	for _, t := range quality.AllTiers() {
		if app == arrSonarr && t == quality.TierPreRelease {
			continue
		}
		out = append(out, arrCatalogItem{Q: arrQualityOf(app, t), Tier: t, HasTier: true})
		for _, x := range extras {
			if x.after == t {
				out = append(out, arrCatalogItem{Q: x.q, Tier: x.tier, HasTier: x.hasTier, Alias: x.hasTier})
			}
		}
	}
	for i := range out {
		out[i].order = i
	}
	return out
}

// arrQualityByRef finds a quality the way an app names it: by its name
// (the TRaSH Guides and Recyclarr go by name, whatever the number), and only
// when no name is given by its number.
func arrQualityByRef(app arrApp, id int, name string) (arrCatalogItem, bool) {
	cat := arrCatalog(app)
	if n := strings.TrimSpace(name); n != "" {
		for _, c := range cat {
			if strings.EqualFold(c.Q.Name, n) {
				return c, true
			}
		}
	}
	if strings.TrimSpace(name) != "" {
		return arrCatalogItem{}, false // a name Mediarium has never heard of is not guessed from a number
	}
	for _, c := range cat {
		if c.Q.ID == id {
			return c, true
		}
	}
	return arrCatalogItem{}, false
}
