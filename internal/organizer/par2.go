package organizer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	return mains, nil
}

// VerifyResult reports whether the recovery set thinks its target files
// are intact.
type VerifyResult struct {
	OK     bool
	Output string
}

// Verify runs `par2 verify` against a main .par2 index.
func (r *Repairer) Verify(par2File string) (VerifyResult, error) {
	cmd := exec.Command(r.binary(), "verify", par2File)
	out, err := cmd.CombinedOutput()
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
	cmd := exec.Command(r.binary(), "repair", par2File)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("par2 repair %s: %w: %s", par2File, err, truncateOutput(out))
	}
	return nil
}
