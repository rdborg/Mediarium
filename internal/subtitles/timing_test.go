package subtitles

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func srt(starts []int64, scale float64, offset int64) []byte {
	var b strings.Builder
	for i, s := range starts {
		a := int64(math.Round(float64(s)*scale)) + offset
		fmt.Fprintf(&b, "%d\n%s --> %s\nLine %d\n\n", i+1, formatStamp(a, "00:00:00,000"), formatStamp(a+1800, "00:00:00,000"), i+1)
	}
	return []byte(b.String())
}

func TestStampsRoundTrip(t *testing.T) {
	cases := []struct {
		in   string
		ms   int64
		back string
	}{
		{"00:01:02,345", 62345, "00:01:02,345"},
		{"01:02:03.004", 3723004, "01:02:03.004"},
		{"02:03.5", 123500, "02:03.500"},
		{"1:00:00,000", 3600000, "01:00:00,000"},
	}
	for _, tc := range cases {
		ms, ok := parseStamp(tc.in)
		if !ok || ms != tc.ms || formatStamp(ms, tc.in) != tc.back {
			t.Errorf("%q: %d %v %q", tc.in, ms, ok, formatStamp(ms, tc.in))
		}
	}
	if _, ok := parseStamp("nonsense"); ok {
		t.Error("nonsense parsed")
	}
}

func TestRetimeKeepsEverythingElse(t *testing.T) {
	in := "1\n00:00:01,000 --> 00:00:02,500 X1:10 Y1:20\n<i>Hello</i>\n\n2\n00:00:03,000 --> 00:00:04,000\nWorld --> again\n"
	out, err := Retime([]byte(in), 1, 1500)
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n00:00:02,500 --> 00:00:04,000 X1:10 Y1:20\n<i>Hello</i>\n\n2\n00:00:04,500 --> 00:00:05,500\nWorld --> again\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	back, _ := Retime(out, 1, -5000)
	if !strings.Contains(string(back), "00:00:00,000 --> 00:00:00,500") {
		t.Fatalf("times below zero stop at zero: %s", back)
	}
	vtt := "WEBVTT\n\n00:01.000 --> 00:02.000\nHi\n"
	if out, _ := Retime([]byte(vtt), 1, 250); !strings.Contains(string(out), "00:01.250 --> 00:02.250") {
		t.Fatalf("vtt: %s", out)
	}
	if _, err := Retime([]byte("no times here"), 1, 10); err != ErrNoCues {
		t.Fatalf("no cues: %v", err)
	}
}

func TestAlign(t *testing.T) {
	// A reference with uneven gaps, like real dialogue.
	var ref []int64
	at := int64(5000)
	for i := 0; i < 400; i++ {
		ref = append(ref, at)
		at += 1500 + int64((i*7919)%5300)
	}
	cases := []struct {
		name   string
		scale  float64
		offset int64
	}{
		{"same", 1, 0},
		{"4.2 seconds late", 1, 4200},
		{"2.37 seconds early", 1, -2370},
		{"made for 25 fps", 23.976 / 25, 900},
	}
	for _, tc := range cases {
		// The target is the reference moved; aligning should undo it.
		target := srt(ref, tc.scale, tc.offset)
		m, err := Align(target, srt(ref, 1, 0))
		if err != nil {
			t.Fatal(err)
		}
		fixed, _ := Retime(target, m.Scale, m.OffsetMs)
		if again, _ := Align(fixed, srt(ref, 1, 0)); math.Abs(float64(again.OffsetMs)) > 20 || again.Scale != 1 || m.Matched < 0.95 {
			t.Errorf("%s: match %+v, after fixing %+v", tc.name, m, again)
		}
	}
	// Two subtitles that have nothing to do with each other barely match.
	var other []int64
	for i := 0; i < 300; i++ {
		other = append(other, int64(i)*3333+int64((i*104729)%2999))
	}
	if m, _ := Align(srt(other, 1, 0), srt(ref, 1, 0)); m.Matched > 0.6 {
		t.Errorf("unrelated subtitles matched %.2f", m.Matched)
	}
}
