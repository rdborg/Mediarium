package api

import (
	"math"
	"net/http/httptest"
	"strconv"
	"testing"
)

// A page number that is far too big is a page past the end, not an overflow
// that turns into a negative slice position.
func TestPagingSurvivesHugePageNumbers(t *testing.T) {
	for _, page := range []int{1, 2, 3, 4, math.MaxInt, math.MaxInt - 1, math.MaxInt/1000 + 2, 0, -5} {
		from, to := pageOf(25, page, 10)
		if from < 0 || to < from || to > 25 {
			t.Errorf("pageOf(25, %d, 10) = %d, %d", page, from, to)
		}
		from, to, pages := pageBounds(25, page, 10)
		if from < 0 || to < from || to > 25 || pages != 3 {
			t.Errorf("pageBounds(25, %d, 10) = %d, %d, %d", page, from, to, pages)
		}
	}
	if from, to := pageOf(25, 3, 10); from != 20 || to != 25 {
		t.Errorf("the last page is %d to %d, want 20 to 25", from, to)
	}
	if from, to := pageOf(0, math.MaxInt, 1000); from != 0 || to != 0 {
		t.Errorf("an empty list gives %d to %d", from, to)
	}
	if from, to, _ := pageBounds(25, 2, 10); from != 10 || to != 20 {
		t.Errorf("the second page is %d to %d, want 10 to 20", from, to)
	}
}

// SABnzbd's start and limit with a limit as big as a number can be must not
// overflow when they are added.
func TestSabPageSurvivesAHugeLimit(t *testing.T) {
	r := httptest.NewRequest("GET", "/sabnzbd/api?mode=queue&start=3&limit="+strconv.Itoa(math.MaxInt), nil)
	start, limit := sabPage(r, 10)
	end := 10
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	if start != 3 || end != 10 {
		t.Errorf("start %d, end %d, want 3 and 10", start, end)
	}
}

// A crash in background work is logged and ends that work, not the program.
func TestBackgroundWorkThatPanicsDoesNotEndTheProgram(t *testing.T) {
	s := &Server{}
	ran := false
	s.background(func() { panic("something unexpected") })
	s.background(func() { ran = true })
	s.bg.Wait()
	if !ran {
		t.Fatal("the other work should still run")
	}
}
