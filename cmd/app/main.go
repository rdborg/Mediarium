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
	"sync"
	"syscall"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/backup"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/logbuf"
	"github.com/rdborg/mediarium/internal/selfupdate"
	"github.com/rdborg/mediarium/internal/store"
	"github.com/rdborg/mediarium/internal/torrentclient"
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
//	go build -ldflags="-X main.defaultOpenSubtitlesAPIKey=... -X main.defaultTraktClientID=... -X main.defaultSimklClientID=..." ./cmd/app
var (
	defaultOpenSubtitlesAPIKey string
	defaultTraktClientID       string
	defaultSimklClientID       string
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
	if len(os.Args) > 1 && os.Args[1] == "config" {
		os.Exit(runConfigCommand(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		if err := resetPassword(os.Args[2:]); err != nil {
			log.Fatalf("mediarium: %v", err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-key" {
		os.Exit(runResetKey(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-two-step" {
		if err := resetTwoStep(os.Args[2:]); err != nil {
			log.Fatalf("mediarium: %v", err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatalf("mediarium: %v", err)
	}
	// After an update, a restore or the Restart button, start the next program
	// in this same process: the container keeps running, so Docker never has
	// to set up its published ports again (some NAS versions fail at that).
	// If that can't be done, exiting lets the restart policy start it.
	if restartWanted != nil && restartWanted() {
		restartInPlace(restartUpdateDir)
	}
}

// Set by run: whether the app stopped to start again, and its update folder.
var (
	restartWanted    func() bool
	restartUpdateDir string
)

func run() error {
	// The log also goes into a small in-memory buffer (secrets taken out), so
	// Settings > System can copy the recent lines for a support request.
	// What goes to stderr (`docker logs`, which people paste into bug reports)
	// is scrubbed the same way.
	logOut := io.MultiWriter(logbuf.ScrubWriter{W: os.Stderr}, logbuf.Default)
	log.SetOutput(logOut)
	// slog lines (some are debug level) follow a level that Settings > Logs can
	// raise while the app runs; they go out the same way.
	logbuf.InstallSlog()
	// The torrent engine logs on its own; send it through the same scrubber.
	torrentclient.SetLogOutput(logOut)

	applyMemoryLimit("/")

	cfg := config.Load()
	for _, problem := range config.Problems() {
		log.Print("warning: " + problem)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.URLBase != "" {
		log.Printf("Serving under %s (URL_BASE)", cfg.URLBase)
	}
	if _, err := httpsec.ParseProxies(cfg.TrustedProxies); err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", cfg.ConfigDir, err)
	}
	// "Restart with automation paused" leaves a note that applies to this start
	// only, like MEDIARIUM_PAUSE_AUTOMATION=1 for one run.
	updateDir := selfupdate.Dir(cfg.ConfigDir)
	restartUpdateDir = updateDir
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
	go store.Optimize(context.Background(), db, true) // statistics for the query planner, without holding up the start

	box, err := loadSecretKey(db, cfg.SecretKeyPath)
	if err != nil {
		return fmt.Errorf("load encryption key: %w", err)
	}
	// A key that is there but belongs to another database would otherwise only
	// show up as searches that quietly find nothing.
	if err := api.CheckSecretKey(db, box); err != nil {
		log.Printf("warning: %v", err)
	}

	server, err := api.New(db, cfg, box, defaultTMDBAPIKey, version)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}
	restartWanted = server.RestartWanted
	// Passwords and keys that can't be read are named in the log (never their
	// values) and on the Health list, so none of them goes quiet.
	server.LogUnreadableSecrets()
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
		SimklClientID: defaultSimklClientID,
	})
	if err := server.SetUpdatePublicKey(updatePublicKey); err != nil {
		log.Printf("warning: %v; installing updates from GitHub is switched off", err)
	}

	// With no administrator yet, the log says how to create one from outside
	// the home network (a one-time setup code; see internal/api/setup.go).
	server.AnnounceSetup()

	scheduler := server.StartAutomation()
	defer func() { stopWithin("the background jobs", scheduler.Stop, stopBudget.left(0)) }()
	// Runs before the scheduler stops (defers run last to first): downloads end and
	// the torrent engine closes, with every item left as the next start resumes from.
	defer func() { server.Shutdown(stopBudget.left(schedulerReserve)) }()

	stopMonitor := server.StartMonitor()
	defer stopMonitor()

	stopAnnounce := server.StartAnnounce()
	defer stopAnnounce()

	stopUpdates := server.StartUpdates()
	defer stopUpdates()

	stopUsageStats := server.StartUsageStats()
	defer stopUsageStats()

	stopWatchdog := server.StartWatchdog()
	defer stopWatchdog()

	stopStreamPause := server.StartStreamPause()
	defer stopStreamPause()

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.Routes(),
		// Slow-header and idle-connection limits. No server-wide read or write
		// timeout: video streaming and backup downloads legitimately take a long
		// time, and a read timeout would cut them off. A slow request body is
		// limited per request instead (api.Routes, read_deadline.go).
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}

	// Live streams stay open until they are closed: end them when the stop
	// starts, so it does not wait for them.
	httpServer.RegisterOnShutdown(server.CloseEventStreams)

	// With the switch on, the settings file in the config folder is applied
	// when it changed since last time. It never stops the start.
	go server.ApplyConfigFileAtStart()

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
		stopServing(httpServer, stopBudget.left(shutdownReserve))
		return nil
	}
}

// Docker waits 10 seconds after the stop signal before it kills a container,
// and the database still has to be closed after the steps of the stop. They
// share one budget, counted from the moment the stop starts, so one slow step
// cannot push the others past the kill.
const (
	// shutdownBudget is how long all the steps of the stop may take together.
	shutdownBudget = 8 * time.Second
	// shutdownReserve is what stays for the later steps while requests that are
	// still running get to finish: the downloads and the torrent engine, then
	// the background jobs.
	shutdownReserve = 5 * time.Second
	// schedulerReserve is what stays for the background jobs while the downloads
	// and the torrent engine stop.
	schedulerReserve = 2 * time.Second
)

// stopBudget is the shared time of the stop.
var stopBudget shutdownClock

// shutdownClock counts the shared stop time from its first use.
type shutdownClock struct {
	once  sync.Once
	start time.Time
	now   func() time.Time // time.Now when nil
}

func (c *shutdownClock) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// left is the time a step may take: what is left of the budget less reserve
// for the steps after it, but never less than a short moment, so each step
// still gets to try.
func (c *shutdownClock) left(reserve time.Duration) time.Duration {
	c.once.Do(func() { c.start = c.clock() })
	d := shutdownBudget - c.clock().Sub(c.start) - reserve
	return max(d, 300*time.Millisecond)
}

// stopWithin runs stop and gives up waiting for it after grace, so what comes
// next (closing the database) always happens. The slow one is left running; the
// program is exiting anyway.
func stopWithin(what string, stop func(), grace time.Duration) {
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(grace):
		log.Printf("Stopped waiting for %s after %s; closing anyway.", what, grace)
	}
}

// stopServing stops taking requests and gives the running ones up to grace to
// finish. A video stream or a big upload can go on for hours, so whatever is
// still open after that is cut off: the program is stopping anyway, and
// waiting longer would only get it killed in the middle of closing the database.
func stopServing(srv *http.Server, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Closing the connections that were still open after %s: %v", grace, err)
		if err := srv.Close(); err != nil {
			log.Printf("warning: could not close the remaining connections: %v", err)
		}
	}
}
