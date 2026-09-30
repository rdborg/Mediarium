package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/store"
)

// resetPassword implements `app reset-password <username>` — recovery for a
// forgotten admin password. There is deliberately no HTTP equivalent: an
// unauthenticated reset endpoint would be a hole, so this needs shell
// access to the container, same trust boundary as the DB file itself.
func resetPassword(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: app reset-password <username>")
	}
	username := args[0]

	cfg := config.Load()
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database at %s: %w", cfg.DBPath, err)
	}
	defer db.Close()

	password, err := promptPassword("New password for " + username + ": ")
	if err != nil {
		return err
	}
	confirm, err := promptPassword("Confirm new password: ")
	if err != nil {
		return err
	}
	if password != confirm {
		return fmt.Errorf("the passwords do not match")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	if err := auth.New(db).ResetPassword(username, password); err != nil {
		return err
	}
	fmt.Printf("Password for %q updated. Any existing sessions were signed out.\n", username)
	return nil
}

// promptPassword reads a line from stdin, hiding what's typed when stdin is
// a terminal. Echo is toggled with stty rather than pulling in
// golang.org/x/term for a single recovery command — if stty isn't
// available the password is simply read without hiding, which still works.
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	interactive := false
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		interactive = exec.Command("stty", "-echo").Run() == nil
	}
	if interactive {
		defer func() {
			_ = exec.Command("stty", "echo").Run()
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
