package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

func formatProfile() quality.Profile {
	hdr := quality.CustomFormat{ID: 1, Name: "HDR", App: quality.AppRadarr, Specs: []quality.FormatSpec{{Name: "HDR", Kind: quality.SpecReleaseTitle, Value: `\bHDR\b`}}}
	hdr.Compile()
	p := quality.Presets()[quality.Preset1080p]
	p.UpgradeAllowed = true
	p.FormatScores = map[int64]int{1: 100}
	p.CutoffFormatScore = 100
	return quality.AttachFormats(p, map[int64]*quality.CustomFormat{1: &hdr})
}

// The release lists and the import share one idea of "a better version": a
// release the search calls an upgrade is never turned away at the import.
func TestImportBetterAgreesWithTheListsDefinitionOfAnUpgrade(t *testing.T) {
	p := formatProfile()
	const current = "Heat.1995.1080p.WEB-DL.x264-GRP"
	tests := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"higher quality", "Heat.1995.1080p.BluRay.x264-GRP", true},
		{"same quality, higher custom-format score", "Heat.1995.1080p.WEB-DL.HDR.x264-GRP", true},
		{"same quality, same score", "Heat.1995.1080p.WEB-DL.x264-OTHER", false},
		{"lower quality", "Heat.1995.720p.HDTV.x264-GRP", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := incomingRelease{title: tt.candidate, tier: quality.Classify(parser.Parse(tt.candidate))}
			cur := currentFile{tier: quality.TierWebDL1080p, release: current}
			got := importBetter(p, in, cur)
			if got != tt.want {
				t.Errorf("importBetter = %v, want %v", got, tt.want)
			}
			// What an automatic upgrade search calls an upgrade is never refused.
			if p.IsUpgradeOverRel(cur.tier, cur.release, tt.candidate, parser.Parse(tt.candidate)) && !got {
				t.Errorf("the search calls it an upgrade but the import would refuse it")
			}
		})
	}
	// A profile that does not look for format upgrades keeps the old, tier-only answer.
	p.UpgradeAllowed = false
	in := incomingRelease{title: "Heat.1995.1080p.WEB-DL.HDR.x264-GRP", tier: quality.TierWebDL1080p}
	if importBetter(p, in, currentFile{tier: quality.TierWebDL1080p, release: current}) {
		t.Error("with better versions off in the profile, a same-quality release is not better")
	}
}

func TestRejectionReasonsForKeepsAndTheBestMatch(t *testing.T) {
	p := formatProfile()
	titles := []string{"Heat.1995.1080p.BluRay.x264-GRP", "Heat.1995.1080p.WEB-DL.HDR.x264-GRP"}
	for _, tc := range []struct {
		name       string
		keeps      importKeeps
		downloaded bool
		wantNote   bool
		wantBest   bool
	}{
		{"nothing keeps the file", keepsNothing, true, false, true},
		{"the title is left alone", keepsAlone, true, true, false},
		{"the quality of the file is unknown", keepsUnknown, true, true, false},
		{"no file yet: the setting does not matter", keepsAlone, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			found := results(titles...)
			payloads := make([]searchResultPayload, len(found))
			noted := 0
			for i := range found {
				payloads[i].Title = found[i].Title
				reasons := rejectionReasonsFor(found[i].Title, p, tc.downloaded, "", "", tc.keeps)
				payloads[i].addReasonList(reasons)
				for _, r := range reasons {
					if strings.HasPrefix(r.Text, "Not picked automatically: ") {
						noted++
						if !strings.Contains(r.Text, "You can still pick it by hand and replace the file.") || r.Kind != "upgrade" {
							t.Errorf("unexpected note %+v", r)
						}
					}
				}
			}
			if (noted > 0) != tc.wantNote {
				t.Errorf("notes = %d, want note %v", noted, tc.wantNote)
			}
			markBestMatch(payloads, found, p)
			best := false
			for _, pl := range payloads {
				best = best || pl.Best
			}
			if best != tc.wantBest {
				t.Errorf("best match marked = %v, want %v", best, tc.wantBest)
			}
		})
	}
}

func TestNotPickedReasonTexts(t *testing.T) {
	alone, ok1 := notPickedReason(keepsAlone)
	unknown, ok2 := notPickedReason(keepsUnknown)
	_, ok3 := notPickedReason(keepsNothing)
	if !ok1 || !ok2 || ok3 {
		t.Fatalf("ok: %v %v %v", ok1, ok2, ok3)
	}
	if alone.Text != "Not picked automatically: this title is set to leave what you have alone. You can still pick it by hand and replace the file." {
		t.Errorf("text: %q", alone.Text)
	}
	if alone.Text == unknown.Text || !strings.Contains(unknown.Text, "can't tell the quality") {
		t.Errorf("text: %q", unknown.Text)
	}
}

func TestStageNameStaysBesideTheFileAndFitsTheName(t *testing.T) {
	long := strings.Repeat("é", 200) + ".mkv"
	for _, base := range []string{"Film (2000).mkv", long} {
		dest := filepath.Join("lib", "Film (2000)", base)
		got := stageName(dest)
		name := filepath.Base(got)
		if filepath.Dir(got) != filepath.Dir(dest) {
			t.Errorf("not beside the file: %s", got)
		}
		if !strings.HasSuffix(name, ".mediarium-tmp") || len(name) > 255 || !utf8.ValidString(name) {
			t.Errorf("bad staging name %q (%d bytes)", name, len(name))
		}
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if strings.HasPrefix(name, stem+".") {
			t.Errorf("the staging name %q could be taken for a file named after %q", name, base)
		}
		if stageName(dest) == got {
			t.Errorf("two stagings of one file got the same name")
		}
	}
}

func TestHandPickedHelpers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "b.mkv")
	if !handPickedSwap(true, file, []string{"", file}) {
		t.Error("the title's own file at the destination is swapped")
	}
	if handPickedSwap(false, file, []string{file}) {
		t.Error("not asked: nothing is swapped")
	}
	if handPickedSwap(true, other, []string{file}) {
		t.Error("another name is not an in-place swap")
	}
	if handPickedSwap(true, file, []string{filepath.Join(dir, "gone.mkv")}) {
		t.Error("a file that is not there is not swapped")
	}
	if !handPickedReplaces(true, []string{file}) || handPickedReplaces(true, []string{"", filepath.Join(dir, "gone.mkv")}) || handPickedReplaces(false, []string{file}) {
		t.Error("handPickedReplaces")
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if handPickedReplaces(true, []string{filepath.Join(dir, "folder")}) {
		t.Error("a folder is never a file to replace")
	}
}

func TestKeptDataSentences(t *testing.T) {
	for data, want := range map[string]string{
		"deleted": "The downloaded files of Rel were deleted.",
		"seeding": "The release Rel is still seeding, so its downloaded files stay until seeding is done.",
		"client":  "The release Rel stays in your download client, which keeps its files.",
	} {
		if got := keptDataSentence(data, "Rel"); got != want {
			t.Errorf("%s: %q", data, got)
		}
	}
}

// The temporary file of a swap that was cut short is cleared by the next swap
// of the same file once it is an hour old, and nothing else is.
func TestStaleSwapFilesAreCleared(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "Heat (1995).mkv")
	old := time.Now().Add(-2 * time.Hour)
	put := func(name string, mod time.Time) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("half"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := put(".0badc0de-Heat (1995).mkv.mediarium-tmp", old)
	recent := put(".1badc0de-Heat (1995).mkv.mediarium-tmp", time.Now())
	other := put(".2badc0de-Alien (1979).mkv.mediarium-tmp", old)
	video := put("Heat (1995).mkv", old)
	notHex := put(".zzzzzzzz-Heat (1995).mkv.mediarium-tmp", old)

	removeStaleStages(dest, time.Now())

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the old temporary file should be gone: %v", err)
	}
	for name, p := range map[string]string{"recent file": recent, "other title's file": other, "the video": video, "odd name": notHex} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the %s should stay: %v", name, err)
		}
	}
}
