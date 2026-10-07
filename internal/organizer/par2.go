package organizer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// par2Timeout is the longest the par2 tool may run for one recovery set. A
// repair of a large release takes a while, but a tool that has run this long
// is stuck (or was handed something hostile), and would otherwise hold the
// download line for ever.
const par2Timeout = 3 * time.Hour

// Repairer verifies and repairs downloads using PAR2 recovery data (the
// "verify (PAR2 repair for Usenet)" step). Shells out to the
// official par2cmdline `par2` binary rather than a pure-Go implementation
// (no mature one exists) — decided during the pre-build Q&A.
type Repairer struct {
	BinaryPath string // defaults to "par2" if empty
}

func NewRepairer() *Repairer { return &Repairer{BinaryPath: "par2"} }

func (r *Repairer) binary() string {
	if r.BinaryPath != "" {
		return r.BinaryPath
	}
	return "par2"
}

func (r *Repairer) Available() bool {
	_, err := exec.LookPath(r.binary())
	return err == nil
}

// FindMainPar2Files returns each archive set's main index file (e.g.
// "movie.par2"), excluding recovery volume files ("movie.vol000+01.par2")
// which the main index references and par2 reads automatically.
func FindMainPar2Files(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var mains []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".par2") {
			continue
		}
		if strings.Contains(lower, ".vol") {
			continue // recovery volume, not a main index
		}
		mains = append(mains, filepath.Join(dir, name))
	}
	return onePerSet(mains), nil
}

// onePerSet keeps one PAR2 file per recovery set. In an obfuscated release
// every volume has a random name without ".vol", so each looks like a main
// index; verifying the whole release once per volume would take hours.
func onePerSet(files []string) []string {
	if len(files) < 2 {
		return files
	}
	type pick struct {
		path string
		size int64
	}
	best := map[string]pick{}
	var order []string
	var unknown []string
	for _, f := range files {
		set := par2SetID(f)
		if set == "" {
			unknown = append(unknown, f)
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		cur, ok := best[set]
		if !ok {
			order = append(order, set)
		}
		if !ok || info.Size() < cur.size {
			best[set] = pick{f, info.Size()}
		}
	}
	out := make([]string, 0, len(order)+len(unknown))
	for _, set := range order {
		out = append(out, best[set].path)
	}
	return append(out, unknown...)
}

// otherFiles lists every other file in par2File's folder. par2 is given
// them all, so it also finds data and recovery volumes that don't carry the
// names it expects.
func otherFiles(par2File string) []string {
	dir := filepath.Dir(par2File)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && e.Name() != filepath.Base(par2File) {
			out = append(out, e.Name())
		}
	}
	return out
}

// VerifyResult reports whether the recovery set thinks its target files
// are intact.
type VerifyResult struct {
	OK     bool
	Output string
}

// Verify runs `par2 verify` against a main .par2 index.
func (r *Repairer) Verify(par2File string) (VerifyResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), par2Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.binary(), append([]string{"verify", "--", par2File}, otherFiles(par2File)...)...)
	cmd.Dir = filepath.Dir(par2File)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return VerifyResult{}, fmt.Errorf("run par2 verify: it took too long and was stopped: %w", ctx.Err())
	}
	if err == nil {
		return VerifyResult{OK: true, Output: string(out)}, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		// par2cmdline exits non-zero when files are damaged/incomplete —
		// that's a normal "not ok" result, not a tool failure.
		return VerifyResult{OK: false, Output: string(out)}, nil
	}
	return VerifyResult{}, fmt.Errorf("run par2 verify: %w", err)
}

// Repair runs `par2 repair` against a main .par2 index.
func (r *Repairer) Repair(par2File string) error {
	ctx, cancel := context.WithTimeout(context.Background(), par2Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.binary(), append([]string{"repair", "--", par2File}, otherFiles(par2File)...)...)
	cmd.Dir = filepath.Dir(par2File)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("par2 repair %s: it took too long and was stopped: %w", par2File, ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("%s (par2: %w)", par2Problem(out), err)
	}
	return nil
}

// par2Problem turns par2's output into one plain sentence. The tool prints
// a progress line for every percent, which said nothing useful in a log.
func par2Problem(out []byte) string {
	text := strings.ReplaceAll(string(out), "\r", "\n")
	var need string
	notPossible := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "You need ") && strings.Contains(line, "recovery block"):
			need = strings.TrimSuffix(strings.TrimPrefix(line, "You need "), " to be able to repair.")
		case strings.Contains(line, "Repair is not possible"):
			notPossible = true
		}
	}
	switch {
	case need != "":
		return "PAR2 can't repair this download: it needs " + need + " than the release has"
	case notPossible:
		return "PAR2 can't repair this download: too much of it is missing"
	}
	var last []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "%") || strings.HasPrefix(line, "Loading") || strings.HasPrefix(line, "Scanning") {
			continue
		}
		last = append(last, line)
	}
	if len(last) > 3 {
		last = last[len(last)-3:]
	}
	if len(last) == 0 {
		return "PAR2 couldn't repair this download"
	}
	return "PAR2 couldn't repair this download: " + strings.Join(last, " ")
}
