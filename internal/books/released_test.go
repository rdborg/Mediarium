package books

import (
	"testing"
	"time"
)

func TestReleased(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name string
		book Book
		want bool
	}{
		{"no date, no year", Book{}, true},
		{"out years ago", Book{Year: 1937}, true},
		{"this year, no date", Book{Year: 2026}, true},
		{"announced for next year", Book{Year: 2027}, false},
		{"out today", Book{ReleaseDate: "2026-10-06"}, true},
		{"out tomorrow", Book{ReleaseDate: "2026-10-07", Year: 2026}, false},
		{"the date wins over the year", Book{ReleaseDate: "2026-01-02", Year: 2027}, true},
	}
	for _, tt := range tests {
		if got := tt.book.Released(now); got != tt.want {
			t.Errorf("%s: Released = %v, want %v", tt.name, got, tt.want)
		}
	}
}
