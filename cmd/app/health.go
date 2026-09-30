package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/selfupdate"
)

// markHealthyWhenAnswering clears the start counter of an installed update
// once it has been running for selfupdate.HealthyAfter and its web server
// answers. The container's entrypoint counts every start of the installed
// program that has not got this far; three in a row and it puts the program
// aside and starts the one inside the image instead. Only ever used when this
// process is the installed program.
func markHealthyWhenAnswering(ctx context.Context, port int, dir string) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(selfupdate.HealthyAfter):
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/version", port)
	for {
		if resp, err := client.Get(url); err == nil {
			ok := resp.StatusCode == http.StatusOK
			resp.Body.Close()
			if ok {
				selfupdate.MarkHealthy(dir)
				slog.Info("The installed update is running and answering; it is now trusted")
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
