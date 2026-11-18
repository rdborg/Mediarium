package organizer

import "testing"

func TestOnlyAttributeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		want bool
	}{
		{"attribute only", "7-Zip 26.01\nERROR: Cannot set file attribute : errno=1 : Operation not permitted : /d/a.mkv\n", true},
		{"two attribute lines", "ERROR: Cannot set file attribute : x\nERROR: Cannot set file time : y\n", true},
		{"attribute and CRC", "ERROR: Cannot set file attribute : x\nERROR: CRC Failed : a.mkv\n", false},
		{"no error line", "Everything is Ok\n", false},
		{"other error", "ERROR: Wrong password : a.mkv\n", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := onlyAttributeErrors([]byte(tc.out)); got != tc.want {
				t.Errorf("onlyAttributeErrors = %v, want %v", got, tc.want)
			}
		})
	}
}
