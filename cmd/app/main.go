// Command app is the entrypoint for Mediarium: a single binary
// that starts the web UI/API and all background workers (indexer engine,
// download clients, automation loop, etc.) in one process.
//
// See docs/ for installation, configuration and the feature overview.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ryanborg/mediarium/internal/api"
	"github.com/ryanborg/mediarium/internal/backup"
	"github.com/ryanborg/mediarium/internal/config"
	"github.com/ryanborg/mediarium/internal/crypto"
	"github.com/ryanborg/mediarium/internal/store"
)

// defaultTMDBAPIKey is empty in source control on purpose (no secrets in
// the repo) and is meant to be injected at build time by
// whoever produces an official release binary, e.g.:
//
//	go build -ldflags="-X main.defaultTMDBAPIKey=xxxxx" ./cmd/app
//
// This is how Radarr/Sonarr-style apps ship "just works, no setup" TMDB
// lookups without a personal API key ever touching the public repo: the
// key lives only in the maintainer's own build pipeline/release secrets,
// never in a commit. Anyone building from source themselves gets an empty
// default and enters their own key in Settings, exactly as today — this
// only removes that step from *official* pre-built releases, once one
// exists to bake a key into.
var defaultTMDBAPIKey string

// The other two app-wide identifiers are injected the same way. All three
// are the release maintainer's own registrations of Mediarium with each
// service, shared by every user of an official build, so nobody has to sign
// up for TMDB, OpenSubtitles or Trakt themselves. Builds from source leave
// them empty and the app asks for them instead.
//
//	go build -ldflags="-X main.defaultOpenSubtitlesAPIKey=... -X main.defaultTraktClientID=..." ./cmd/app
var (
	defaultOpenSubtitlesAPIKey string
	defaultTraktClientID       string
)

// version identifies the running build (the "version-pinned Docker tags,
// changelog per release" update policy). Set at build time via
// ldflags, e.g.:
//
//	go build -ldflags="-X main.version=$(cat VERSION)" ./cmd/app
//
// The number lives in the VERSION file at the repository root (semantic
// versioning, no leading "v"; version_test.go checks it parses). The
// Dockerfile and the release workflow read it and pass it here. A plain
// `go build`/`go run` without ldflags reports "dev".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		if err := resetPassword(os.Args[2:]); err != nil {
			log.Fatalf("mediarium: %v", err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatalf("mediarium: %v", err)
	}
}

func run() error {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", cfg.ConfigDir, err)
	}
	// Media folders are yours: if one cannot be created or written the app still
	// starts and the dashboard flags it, rather than refusing to run.
	for _, dir := range []string{cfg.DownloadsIncomplete, cfg.DownloadsComplete, cfg.MoviesDir, cfg.TVDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("warning: cannot create %s (is the folder mapped and writable?): %v", dir, err)
		}
	}

	// A restore uploaded through the UI is only staged while the app runs;
	// it is swapped in here, before the database is opened, because the live
	// files cannot be replaced under an open connection. Nothing is deleted:
	// the files being replaced move to before-restore-<timestamp>/.
	restored, err := backup.ApplyPending(cfg.ConfigDir, time.Now())
	if err != nil {
		return fmt.Errorf("apply pending restore: %w", err)
	}
	if restored.Message != "" {
		log.Print(restored.Message)
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database at %s: %w", cfg.DBPath, err)
	}
	defer db.Close()

	box, err := crypto.LoadOrCreateKey(cfg.SecretKeyPath)
	if err != nil {
		return fmt.Errorf("load encryption key: %w", err)
	}

	server, err := api.New(db, cfg, box, defaultTMDBAPIKey, version)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	server.SetBuiltinKeys(api.BuiltinKeys{
		TMDB:          defaultTMDBAPIKey,
		OpenSubtitles: defaultOpenSubtitlesAPIKey,
		TraktClientID: defaultTraktClientID,
	})

	scheduler := server.StartAutomation()
	defer scheduler.Stop()

	stopMonitor := server.StartMonitor()
	defer stopMonitor()

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.Routes(),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// After a restore is staged the app has to restart to apply it. Stopping
	// through the normal shutdown path closes the database cleanly and exits
	// 0; the container's restart policy (docker-compose.yml uses
	// "unless-stopped") brings it back. Without a restart policy the user
	// starts it again by hand and the restore is applied on that start.
	server.SetExitFunc(stop)

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("Mediarium listening on %s (db: %s)", httpServer.Addr, filepath.Clean(cfg.DBPath))
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Println("Mediarium shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}
