package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
)

func TestStartupHint(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string // a part of the hint; "" means no hint
	}{
		{"nil", nil, ""},
		{"port in use", errors.New("serve: listen tcp :8264: bind: address already in use"), "already using Mediarium's port"},
		{"port in use typed", fmt.Errorf("serve: %w", syscall.EADDRINUSE), "already using Mediarium's port"},
		{"port in use on windows", errors.New("serve: listen tcp :8264: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted."), "already using Mediarium's port"},
		{"low port", errors.New("serve: listen tcp :80: bind: permission denied"), "choose a higher one"},
		{"database folder", errors.New("open database at /config/mediarium.db: open sqlite: unable to open database file: out of memory (14)"), "config folder exists"},
		{"database disk full", errors.New("open database at /config/mediarium.db: write /config/mediarium.db-wal: no space left on device"), "disk is full"},
		{"config folder", errors.New("create directory /config: mkdir /config: permission denied"), "not allowed to use that file or folder"},
		{"own sentence", errors.New("URL_BASE must start with a slash"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startupHint(tt.err)
			if tt.want == "" {
				if got != "" {
					t.Fatalf("startupHint = %q, want none", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("startupHint = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}
