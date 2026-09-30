package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/music"
)

func TestPickMusicRelease(t *testing.T) {
	lossy := music.Profile{ID: 2, Name: music.PresetLossy, Allowed: []music.Tier{music.TierAAC256, music.TierMP3320}, Cutoff: music.TierMP3320, UpgradeAllowed: true}
	lossless := music.Profile{ID: 1, Name: music.PresetLossless, Allowed: []music.Tier{music.TierFLAC, music.TierFLAC24}, Cutoff: music.TierFLAC, UpgradeAllowed: true,
		Fallback: []int64{2}, FallbackProfiles: []music.Profile{lossy}}
	artist := music.Artist{Name: "Fixture Band"}
	missing := music.Album{Title: "First Album", ReleaseDate: "1999-03-01", Status: music.StatusMissing}
	onFallback := missing
	onFallback.Status, onFallback.Quality = music.StatusDownloaded, string(music.TierMP3320)
	atCutoff := missing
	atCutoff.Status, atCutoff.Quality = music.StatusDownloaded, string(music.TierFLAC)

	const (
		flac    = "Fixture Band - First Album (1999) [FLAC]"
		flac24  = "Fixture Band - First Album (1999) [FLAC 24bit]"
		mp3     = "Fixture Band - First Album (1999) [MP3 320]"
		mp3low  = "Fixture Band - First Album (1999) [MP3 192]"
		other   = "Fixture Band - Other Album (1999) [FLAC]"
		torrent = "Fixture Band - First Album (1999) [FLAC] (torrent)"
	)
	res := func(title string, p indexers.Protocol) indexers.Result {
		return indexers.Result{Title: title, Protocol: p, DownloadURL: "u:" + title}
	}
	cases := []struct {
		name       string
		results    []indexers.Result
		album      music.Album
		blocked    []string
		sources    string
		want       string
		acceptable int
		fallback   int
	}{
		{"FLAC 24 beats FLAC, both beat the fallback", []indexers.Result{res(mp3, "usenet"), res(flac, "usenet"), res(flac24, "usenet")}, missing, nil, "", flac24, 3, 1},
		{"only MP3 320: the fallback takes it", []indexers.Result{res(mp3, "usenet"), res(mp3low, "usenet"), res(other, "usenet")}, missing, nil, "", mp3, 1, 1},
		{"nothing acceptable", []indexers.Result{res(mp3low, "usenet"), res(other, "usenet")}, missing, nil, "", "", 0, 0},
		{"blocklisted FLAC is skipped", []indexers.Result{res(flac, "usenet"), res(mp3, "usenet")}, missing, []string{flac}, "", mp3, 1, 1},
		{"Usenet preferred on a tie", []indexers.Result{res(torrent, "torrent"), res(flac, "usenet")}, missing, nil, "", flac, 2, 0},
		{"Usenet only drops torrents", []indexers.Result{res(torrent, "torrent")}, missing, nil, sourcesUsenet, "", 0, 0},
		{"a fallback MP3 on disk is replaced by FLAC", []indexers.Result{res(mp3, "usenet"), res(flac, "usenet")}, onFallback, nil, "", flac, 1, 0},
		{"FLAC on disk is at the cutoff", []indexers.Result{res(flac24, "usenet")}, atCutoff, nil, "", "", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked := map[string]bool{}
			for _, b := range tc.blocked {
				blocked[blocklist.Key(b)] = true
			}
			best, v := pickMusicRelease(tc.results, blocked, tc.sources, artist, tc.album, lossless, albumCurrentTier(tc.album))
			got := ""
			if best != nil {
				got = best.Title
			}
			if got != tc.want || v.acceptable != tc.acceptable || v.fallbackOnly != tc.fallback {
				t.Fatalf("got %q (acceptable %d, fallback %d; reasons %v), want %q (%d, %d)", got, v.acceptable, v.fallbackOnly, v.reasons, tc.want, tc.acceptable, tc.fallback)
			}
		})
	}
}
