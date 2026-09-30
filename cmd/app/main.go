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
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/logbuf"
	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/store"
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

// updatePublicKey is the key that signs official releases (base64 of a raw
// ed25519 public key). "Update now" installs a release only when the
// sha256sums.txt.sig file that comes with it verifies against this key. A
// fork that signs its own releases builds with
//
//	-ldflags "-X main.updatePublicKey=<base64 public key>"
//
// Only the public half belongs here; the private half stays with whoever
// publishes releases (see docs/RELEASING.md).
var updatePublicKey = "HM7xZn1frF+sxwYVWD/tyt+53pKr3vMtQ813g0Ccn6M="

func main() {
	if handled, code := handleProbeFlags(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
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
	// The log also goes into a small in-memory buffer (secrets taken out), so
	// Settings > System can copy the recent lines for a support request.
	// What goes to stderr (`docker logs`, which people paste into bug reports)
	// is scrubbed the same way.
	log.SetOutput(io.MultiWriter(logbuf.ScrubWriter{W: os.Stderr}, logbuf.Default))

	cfg := config.Load()
	if _, err := httpsec.ParseProxies(cfg.TrustedProxies); err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", cfg.ConfigDir, err)
	}
	// "Restart with automation paused" leaves a note that applies to this start
	// only, like MEDIARIUM_PAUSE_AUTOMATION=1 for one run.
	updateDir := selfupdate.Dir(cfg.ConfigDir)
	if selfupdate.TakeSafeOnce(updateDir) {
		cfg.PauseAutomation = true
		log.Print("Started once in safe mode because a restart with automation paused was asked for.")
	}
	// Media folders are yours: if one cannot be created or written the app still
	// starts and the dashboard flags it, rather than refusing to run.
	for _, dir := range []string{cfg.DownloadsIncomplete, cfg.DownloadsComplete, cfg.MoviesDir, cfg.TVDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("warning: could not create %s (is the folder mapped and writable?): %v", dir, err)
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
	// Download addresses queued by older versions were kept in plain text.
	if n, err := server.QueueRepo.EncryptStoredURLs(); err != nil {
		log.Printf("warning: could not encrypt the saved download addresses: %v", err)
	} else if n > 0 {
		log.Printf("Encrypted %d saved download address(es).", n)
	}
	// Webhook addresses and ntfy topics are the whole credential of a
	// notification target; older versions kept them in plain text.
	if n, err := server.NotifyRepo.MigrateSecrets(); err != nil {
		log.Printf("warning: could not encrypt the saved notification secrets: %v", err)
	} else if n > 0 {
		log.Printf("Encrypted the secrets of %d saved notification target(s).", n)
	}

	server.SetBuiltinKeys(api.BuiltinKeys{
		TMDB:          defaultTMDBAPIKey,
		OpenSubtitles: defaultOpenSubtitlesAPIKey,
		TraktClientID: defaultTraktClientID,
	})
	if err := server.SetUpdatePublicKey(updatePublicKey); err != nil {
		log.Printf("warning: %v; installing updates from GitHub is switched off", err)
	}

	// With no administrator yet, the log says how to create one from outside
	// the home network (a one-time setup code; see internal/api/setup.go).
	server.AnnounceSetup()

	scheduler := server.StartAutomation()
	defer scheduler.Stop()

	stopMonitor := server.StartMonitor()
	defer stopMonitor()

	stopUpdates := server.StartUpdates()
	defer stopUpdates()

	stopWatchdog := server.StartWatchdog()
	defer stopWatchdog()

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.Routes(),
		// Slow-header and idle-connection limits. No read or write timeout:
		// video streaming and backup uploads legitimately take a long time.
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// After a restore is staged the app has to restart to apply it. Stopping
	// through the normal shutdown path closes the database cleanly and exits
	// 0; the container's restart policy (docker-compose.yml uses
	// "unless-stopped") brings it back. Without a restart policy the user
	// starts it again by hand and the restore is applied on that start.
	server.SetExitFunc(stop)

	if selfupdate.RunningPushed(updateDir) {
		go markHealthyWhenAnswering(ctx, cfg.Port, updateDir)
	}

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
