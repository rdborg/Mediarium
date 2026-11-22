package blocklist_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/blocklist"
)

// A release with a capital that is not plain ASCII is still found when it is
// kept (SQLite's lower() would leave it alone).
func TestKeepShortMatchesNonASCIICapitals(t *testing.T) {
	repo, _ := newRepos(t)
	const title = "Étoile.S01E01.GERMAN-GRP"
	if err := repo.Add(blocklist.Entry{ReleaseTitle: title, Reason: "incomplete file"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(blocklist.Entry{ReleaseTitle: "Other.Release-GRP", Reason: "incomplete file"}); err != nil {
		t.Fatal(err)
	}
	n, err := repo.KeepShort("  étoile.s01e01.german-grp ", "incomplete file")
	if err != nil || n != 1 {
		t.Fatalf("dropped %d, err %v", n, err)
	}
	if list, _ := repo.List(); len(list) != 1 || list[0].ReleaseTitle != "Other.Release-GRP" {
		t.Fatalf("only the kept release should go: %+v", list)
	}
	if !repo.KeptShort(title) {
		t.Fatal("the release should be remembered as kept")
	}
}
