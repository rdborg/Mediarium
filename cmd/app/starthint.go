package main

import (
	"errors"
	"strings"
	"syscall"

	"github.com/rdborg/mediarium/internal/plainerror"
)

// startupHint says in plain words what to do about an error that stops
// Mediarium from starting, for the line under it in `docker logs`. The error
// itself is still printed in full; this is only the part a person can act on.
// It is "" when there is nothing useful to add.
func startupHint(err error) string {
	if err == nil {
		return ""
	}
	low := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, syscall.EADDRINUSE) || strings.Contains(low, "address already in use") || strings.Contains(low, "only one usage of each socket address"):
		return "Another program is already using Mediarium's port. Stop that program, or choose another port with APP_PORT (and the same port in your container's port mapping)."
	case strings.Contains(low, "listen tcp") && (strings.Contains(low, "permission denied") || strings.Contains(low, "forbidden by its access permissions")):
		return "Mediarium is not allowed to use that port. Ports below 1024 need extra rights; choose a higher one with APP_PORT."
	case strings.Contains(low, "unable to open database file") || strings.Contains(low, "open database at"):
		if strings.Contains(low, "no space left") || strings.Contains(low, "disk is full") {
			return "The disk is full, so the database could not be opened. Free up some space and start Mediarium again."
		}
		return "Mediarium could not open its database. Check that the config folder exists, has free space, and can be written to by the user Mediarium runs as (PUID and PGID in Docker)."
	}
	if reason := plainerror.Message(err); reason != "" && reason != err.Error() {
		return reason
	}
	return ""
}
