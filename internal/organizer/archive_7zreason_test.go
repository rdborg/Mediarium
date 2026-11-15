package organizer

import (
	"strings"
	"testing"
)

func TestSevenZipReasonShowsTheErrorNotTheBanner(t *testing.T) {
	banner := "7-Zip (z) 26.01 (x64) : Copyright (c) 1999-2026 Igor Pavlov : 2026-04-27\n" +
		"Scanning the drive for archives:\n1 file, 524288000 bytes (500 MiB)\n" + strings.Repeat("Extracting archive: x\n", 40)
	tests := []struct {
		name, out, want string
	}{
		{"memory", banner + "ERROR: Can't allocate required memory!\n", "ERROR: Can't allocate required memory!"},
		{"write error", banner + "ERROR: E:/x/y.mkv\nNo space left on device\n", "No space left on device"},
		{"crc", banner + "Data Error in 'a.mkv'. File is broken\nCRC Failed : a.mkv\n", "CRC Failed : a.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sevenZipReason([]byte(tt.out))
			if !strings.Contains(got, tt.want) {
				t.Fatalf("reason = %q, want it to contain %q", got, tt.want)
			}
			if strings.Contains(got, "Igor Pavlov") {
				t.Fatalf("reason still starts with the banner: %q", got)
			}
		})
	}
	if got := sevenZipReason([]byte(banner + "something odd happened")); !strings.HasSuffix(got, "something odd happened") {
		t.Fatalf("fallback should keep the end of the output, got %q", got)
	}
}
