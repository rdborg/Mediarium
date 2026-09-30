package config

import "testing"

func TestPauseAutomationVariable(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{"no", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{" yes ", true},
		{"on", true},
	}
	for _, tc := range tests {
		t.Run("value "+tc.value, func(t *testing.T) {
			t.Setenv("MEDIARIUM_PAUSE_AUTOMATION", tc.value)
			if got := Load().PauseAutomation; got != tc.want {
				t.Fatalf("MEDIARIUM_PAUSE_AUTOMATION=%q gave %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
